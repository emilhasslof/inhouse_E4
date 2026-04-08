// datagen — fake GSI data generator for local development and testing.
//
// Simulates 10 Dota 2 players sending Game State Integration payloads to the
// local ingest endpoint. Accepts runtime commands on stdin:
//
//	start  — begin a simulated match (payloads sent every second per player)
//	stop   — end the match (sends POST_GAME state, then halts)
//	status — print current match state
//	quit   — exit
//
// NEVER run in production. The binary will refuse to start if FLY_APP_NAME is set.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func main() {
	if os.Getenv("FLY_APP_NAME") != "" {
		log.Fatal("[datagen] FATAL: datagen must never run in production (FLY_APP_NAME is set)")
	}

	target := flag.String("target", "http://localhost:8080", "base URL of the ingest server")
	flag.Parse()

	players := buildPlayers()
	game := &GameState{}

	fmt.Println("[datagen] ready — commands: start, stop, status, quit")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch strings.TrimSpace(scanner.Text()) {
		case "start":
			game.Start(players, *target)
		case "stop":
			game.Stop(players, *target)
		case "status":
			game.PrintStatus()
		case "quit", "q", "exit":
			game.Stop(players, *target)
			fmt.Println("[datagen] bye")
			return
		default:
			fmt.Println("[datagen] commands: start, stop, status, quit")
		}
	}
}

// ── Game simulation ──────────────────────────────────────────────────────────

// GameState holds shared mutable state for a running simulation.
type GameState struct {
	mu        sync.Mutex
	matchID   string
	clockTime int
	running   bool
	cancel    context.CancelFunc
}

func (g *GameState) Start(players []*FakePlayer, target string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		fmt.Println("[datagen] already running — stop first")
		return
	}

	g.matchID = newMatchID()
	g.clockTime = -90 // pre-game countdown starts at -90s
	g.running = true

	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel

	// Reset player state for a fresh match
	for _, p := range players {
		p.reset()
	}

	// Clock goroutine: increments clockTime every second
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				g.mu.Lock()
				g.clockTime++
				g.mu.Unlock()
			}
		}
	}()

	// One goroutine per player
	for _, p := range players {
		p := p
		go p.run(ctx, g, target)
	}

	fmt.Printf("[datagen] match started (ID: %s) — type 'stop' to end\n", g.matchID)
}

func (g *GameState) Stop(players []*FakePlayer, target string) {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		fmt.Println("[datagen] no match running")
		return
	}
	ct := g.clockTime
	mid := g.matchID
	g.cancel()
	g.running = false
	g.mu.Unlock()

	// Send POST_GAME from all players simultaneously
	var wg sync.WaitGroup
	for _, p := range players {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.sendPayload(target, mid, ct, "DOTA_GAMERULES_STATE_POST_GAME")
		}()
	}
	wg.Wait()
	fmt.Printf("[datagen] match %s ended (clock: %ds)\n", mid, ct)
}

func (g *GameState) PrintStatus() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.running {
		fmt.Println("[datagen] no match running")
		return
	}
	fmt.Printf("[datagen] match=%s  clock=%ds\n", g.matchID, g.clockTime)
}

func newMatchID() string {
	return fmt.Sprintf("%d", 7000000000+rand.Int63n(999999999))
}

// ── Fake player ──────────────────────────────────────────────────────────────

// FakePlayer simulates a single Dota 2 player sending GSI payloads.
type FakePlayer struct {
	name     string
	token    string
	hero     string
	team     string // "radiant" or "dire"
	gpmBase  int    // target gold per minute

	// mutable state — only accessed from the player's own goroutine after reset
	kills    int
	deaths   int
	assists  int
	gold     int
	lastHits int
	level    int
	xpos     float64
	ypos     float64
	rng      *rand.Rand
}

func (p *FakePlayer) reset() {
	p.kills = 0
	p.deaths = 0
	p.assists = 0
	p.gold = 600
	p.lastHits = 0
	p.level = 1
	if p.team == "radiant" {
		p.xpos = -4000 + float64(rand.Intn(2000))
		p.ypos = -4000 + float64(rand.Intn(2000))
	} else {
		p.xpos = 2000 + float64(rand.Intn(2000))
		p.ypos = 2000 + float64(rand.Intn(2000))
	}
	p.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func (p *FakePlayer) run(ctx context.Context, state *GameState, target string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state.mu.Lock()
			ct := state.clockTime
			mid := state.matchID
			state.mu.Unlock()

			if ct > 0 { // only simulate during live game, not pre-game
				p.tick(ct)
			}
			p.sendPayload(target, mid, ct, "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS")
		}
	}
}

func (p *FakePlayer) tick(clockTime int) {
	rng := p.rng

	// Gold accrual: base GPM plus noise
	p.gold += p.gpmBase/60 + rng.Intn(5) - 2
	if p.gold < 0 {
		p.gold = 0
	}

	// Level: increases approximately every 2 minutes
	newLevel := 1 + clockTime/120
	if newLevel > 25 {
		newLevel = 25
	}
	if newLevel > p.level {
		p.level = newLevel
	}

	// Last hits: roughly every 15-20 seconds
	if rng.Intn(18) == 0 {
		p.lastHits++
		p.gold += 40 + rng.Intn(20)
	}

	// Kills: ~1 per 5 minutes (0.33% chance per second)
	if rng.Float64() < 0.0033 {
		p.kills++
		p.gold += 250 + rng.Intn(100)
	}

	// Deaths: ~1 per 7 minutes
	if rng.Float64() < 0.0024 {
		p.deaths++
		loss := 100 + rng.Intn(100)
		if p.gold > loss {
			p.gold -= loss
		}
	}

	// Assists: slightly more common than kills
	if rng.Float64() < 0.004 {
		p.assists++
	}

	// Position drift (random walk within map bounds)
	p.xpos += float64(rng.Intn(200)) - 100
	p.ypos += float64(rng.Intn(200)) - 100
	if p.xpos < -7000 { p.xpos = -7000 }
	if p.xpos > 7000  { p.xpos = 7000 }
	if p.ypos < -7000 { p.ypos = -7000 }
	if p.ypos > 7000  { p.ypos = 7000 }
}

// gsiPayload mirrors the JSON structure the GSI handler expects.
type gsiPayload struct {
	Auth   gsiAuth   `json:"auth"`
	Map    gsiMap    `json:"map"`
	Player gsiPlayer `json:"player"`
	Hero   gsiHero   `json:"hero"`
}

type gsiAuth   struct{ Token string `json:"token"` }
type gsiMap    struct {
	MatchID      string `json:"matchid"`
	ClockTime    int    `json:"clock_time"`
	GameTime     int    `json:"game_time"`
	GameState    string `json:"game_state"`
	RadiantScore int    `json:"radiant_score"`
	DireScore    int    `json:"dire_score"`
}
type gsiPlayer struct {
	TeamName string `json:"team_name"`
	Kills    int    `json:"kills"`
	Deaths   int    `json:"deaths"`
	Assists  int    `json:"assists"`
	Gold     int    `json:"gold"`
	GPM      int    `json:"gpm"`
	XPM      int    `json:"xpm"`
	LastHits int    `json:"last_hits"`
	Denies   int    `json:"denies"`
}
type gsiHero struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

func (p *FakePlayer) sendPayload(target, matchID string, clockTime int, gameState string) {
	gpm := p.gpmBase
	if clockTime > 0 && clockTime > 0 {
		// Derive GPM from accumulated gold
		gpm = p.gold * 60 / clockTime
		if gpm > 1000 { gpm = 1000 }
	}

	payload := gsiPayload{
		Auth: gsiAuth{Token: p.token},
		Map: gsiMap{
			MatchID:   matchID,
			ClockTime: clockTime,
			GameTime:  clockTime,
			GameState: gameState,
		},
		Player: gsiPlayer{
			TeamName: p.team,
			Kills:    p.kills,
			Deaths:   p.deaths,
			Assists:  p.assists,
			Gold:     p.gold,
			GPM:      gpm,
			LastHits: p.lastHits,
		},
		Hero: gsiHero{
			Name:  p.hero,
			Level: p.level,
		},
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(target+"/gsi", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[datagen] %s: send error: %v", p.name, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		log.Printf("[datagen] %s: token rejected (401) — is the server seeded?", p.name)
	}
}

// ── Player definitions ───────────────────────────────────────────────────────

func buildPlayers() []*FakePlayer {
	return []*FakePlayer{
		{name: "Spinelli",      token: "datagen-radiant-1", hero: "npc_dota_hero_anti_mage",        team: "radiant", gpmBase: 630},
		{name: "Sku",           token: "datagen-radiant-2", hero: "npc_dota_hero_invoker",           team: "radiant", gpmBase: 540},
		{name: "Jockwe Lamotte",token: "datagen-radiant-3", hero: "npc_dota_hero_storm_spirit",      team: "radiant", gpmBase: 480},
		{name: "Ottosama",      token: "datagen-radiant-4", hero: "npc_dota_hero_spectre",           team: "radiant", gpmBase: 420},
		{name: "HACKERMAN",     token: "datagen-radiant-5", hero: "npc_dota_hero_chen",              team: "radiant", gpmBase: 310},
		{name: "Maddashåååtaaa",token: "datagen-dire-1",    hero: "npc_dota_hero_phantom_assassin",  team: "dire",    gpmBase: 650},
		{name: "Harvey Specter",token: "datagen-dire-2",    hero: "npc_dota_hero_axe",               team: "dire",    gpmBase: 450},
		{name: "Deer",          token: "datagen-dire-3",    hero: "npc_dota_hero_earth_spirit",      team: "dire",    gpmBase: 360},
		{name: "Jointzart",     token: "datagen-dire-4",    hero: "npc_dota_hero_ember_spirit",      team: "dire",    gpmBase: 520},
		{name: "Lacko",         token: "datagen-dire-5",    hero: "npc_dota_hero_shadow_shaman",     team: "dire",    gpmBase: 300},
	}
}
