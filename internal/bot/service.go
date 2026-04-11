// Package bot wraps the Steam + Dota2 clients and exposes a service that the
// HTTP server can use to create lobbies and invite players.
package bot

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	dota2 "github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	steam "github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/steamlang"
	"github.com/paralin/go-steam/steamid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/emilh/inhouse-e4/internal/db"
	"github.com/emilh/inhouse-e4/internal/match"
)

// steamGuardChars is Steam's custom TOTP alphabet.
var steamGuardChars = []byte("23456789BCDFGHJKMNPQRTVWXY")

// generateSteamCode computes a 5-character Steam Guard code from a
// base64-encoded shared_secret.
func generateSteamCode(secret string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decode shared_secret: %w", err)
	}
	ts := time.Now().Unix() / 30
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(ts))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	offset := sum[19] & 0xF
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7FFFFFFF
	result := make([]byte, 5)
	for i := 0; i < 5; i++ {
		result[i] = steamGuardChars[code%uint32(len(steamGuardChars))]
		code /= uint32(len(steamGuardChars))
	}
	return string(result), nil
}

// Service maintains a persistent Steam + Dota2 GC connection and can create
// lobbies and invite players on demand.
type Service struct {
	username   string
	password   string
	totpSecret string
	lobbyName  string
	lobbyPass  string
	gate       *match.Gate

	client *steam.Client
	dota   *dota2.Dota2

	gcReady     chan struct{}
	gcReadyOnce sync.Once

	botAccountID atomic.Uint32
	onConnected  func() // called once on first ConnectedEvent

	// startMu guards startCh, which is recreated for each lobby.
	startMu sync.Mutex
	startCh chan struct{}
}

// New reads credentials from the environment and returns a Service ready to
// be started. Call Start in a goroutine after construction.
func New(gate *match.Gate) *Service {
	username := os.Getenv("STEAM_ACCOUNT_NAME")
	password := os.Getenv("STEAM_PASSWORD")
	if username == "" || password == "" {
		log.Println("[bot] STEAM_ACCOUNT_NAME or STEAM_PASSWORD not set — bot disabled")
		return nil
	}

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	client := steam.NewClient()
	d := dota2.New(client, logger)

	return &Service{
		username:   username,
		password:   password,
		totpSecret: os.Getenv("STEAM_TOTP_SECRET"),
		lobbyName:  getEnvOr("LOBBY_NAME", "E4 Inhouse"),
		lobbyPass:  getEnvOr("LOBBY_PASSWORD", "inhouse"),
		gate:       gate,
		client:     client,
		dota:       d,
		gcReady:    make(chan struct{}),
	}
}

// connectWithRetry dials a CM server in a goroutine and retries every 3s if
// the dial hangs. This works around net.DialTCP having no built-in timeout.
func (s *Service) connectWithRetry(ctx context.Context) {
	connected := make(chan struct{}, 1)
	s.onConnected = func() {
		select {
		case connected <- struct{}{}:
		default:
		}
	}

	attempt := 0
	for {
		attempt++
		log.Printf("[bot] connecting to Steam (attempt %d)...", attempt)

		dialDone := make(chan struct{}, 1)
		go func() {
			s.client.Connect()
			select {
			case dialDone <- struct{}{}:
			default:
			}
		}()

		select {
		case <-connected:
			return // ConnectedEvent fired — success
		case <-dialDone:
			// Dial returned but no ConnectedEvent yet — wait briefly
			select {
			case <-connected:
				return
			case <-time.After(3 * time.Second):
				log.Printf("[bot] no response from CM — retrying (attempt %d)", attempt+1)
			case <-ctx.Done():
				return
			}
		case <-time.After(3 * time.Second):
			log.Printf("[bot] TCP dial timed out — retrying (attempt %d)", attempt+1)
		case <-ctx.Done():
			return
		}
	}
}

// logOn generates a fresh TOTP code (if configured) and calls LogOn.
func (s *Service) logOn() {
	var twoFactorCode string
	if s.totpSecret != "" {
		code, err := generateSteamCode(s.totpSecret)
		if err != nil {
			log.Printf("[bot] generate TOTP code: %v", err)
			return
		}
		twoFactorCode = code
		log.Printf("[bot] logging in with TOTP code: %s", twoFactorCode)
	}
	s.client.Auth.LogOn(&steam.LogOnDetails{
		Username:               s.username,
		Password:               s.password,
		TwoFactorCode:          twoFactorCode,
		ShouldRememberPassword: true,
	})
}

// Start connects to Steam and runs the event loop. It blocks until ctx is
// cancelled or the connection is permanently lost. Run it in a goroutine.
func (s *Service) Start(ctx context.Context) {
	go func() {
		for event := range s.client.Events() {
			switch e := event.(type) {

			case *steam.ConnectedEvent:
				log.Println("[bot] connected to Steam, logging in...")
				if s.onConnected != nil {
					s.onConnected()
				}
				s.logOn()

			case *steam.LoggedOnEvent:
				log.Printf("[bot] logged in (steamID: %d)", e.ClientSteamId)
				s.botAccountID.Store(uint32(e.ClientSteamId))
				s.client.GC.SetGamesPlayed(uint64(dota2.AppID))
				s.dota.SayHello()
				// Retry SayHello every 10s until the GC acknowledges us.
				go func() {
					t := time.NewTicker(10 * time.Second)
					defer t.Stop()
					for {
						select {
						case <-s.gcReady:
							return
						case <-t.C:
							log.Println("[bot] GC not ready yet — retrying SayHello")
							s.dota.SayHello()
						}
					}
				}()

			case *events.GCConnectionStatusChanged:
				log.Printf("[bot] GC status: %v", e.NewState)
				if e.NewState == protocol.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION {
					s.gcReadyOnce.Do(func() { close(s.gcReady) })
				}

			case *events.ChatMessage:
				if e.GetText() == "!start" {
					log.Printf("[bot] !start received from %s", e.GetPersonaName())
					s.startMu.Lock()
					ch := s.startCh
					s.startMu.Unlock()
					if ch != nil {
						select {
						case ch <- struct{}{}:
						default:
						}
					}
				}

			case *steam.FriendStateEvent:
				if e.Relationship == steamlang.EFriendRelationship_RequestRecipient {
					log.Printf("[bot] incoming friend request from %d — accepting", e.SteamId)
					s.client.Social.AddFriend(e.SteamId)
				}

			case *steam.LogOnFailedEvent:
				if e.Result == steamlang.EResult_TwoFactorCodeMismatch && s.totpSecret != "" {
					log.Println("[bot] TOTP code mismatch — waiting for next window and retrying...")
					remaining := time.Now().Unix() % 30
					time.Sleep(time.Duration(30-remaining+1) * time.Second)
					s.logOn()
				} else {
					log.Printf("[bot] login failed: %v", e.Result)
				}

			case *steam.DisconnectedEvent:
				log.Println("[bot] disconnected from Steam — reconnecting in 5s...")
				time.AfterFunc(5*time.Second, func() {
					if ctx.Err() == nil {
						addr := s.client.Connect()
						log.Printf("[bot] reconnecting to Steam at %v", addr)
					}
				})

			case error:
				log.Printf("[bot] error: %v", e)
			}
		}
	}()

	// Connect in a goroutine — net.DialTCP has no timeout and can block for
	// minutes if a CM server is unresponsive. We retry every 15s so a single
	// stale dial doesn't hold us up.
	go s.connectWithRetry(ctx)

	<-ctx.Done()
	log.Println("[bot] context cancelled, disconnecting")
	s.client.Disconnect()
}

// CreateLobbyAndInvite waits for the GC to be ready, creates a practice lobby,
// kicks the bot out of its team slot so it doesn't block a player slot, sends
// lobby invites to each player, then listens for !start in lobby chat. When
// !start is received the gate opens and the lobby launches. Designed to run in
// a goroutine — errors are logged, not returned.
func (s *Service) CreateLobbyAndInvite(players []db.Player) {
	select {
	case <-s.gcReady:
	case <-time.After(60 * time.Second):
		log.Println("[bot] timed out waiting for GC — cannot create lobby")
		return
	}

	gameMode := uint32(protocol.DOTA_GameMode_DOTA_GAMEMODE_AP)
	visibility := protocol.DOTALobbyVisibility_DOTALobbyVisibility_Public
	details := &protocol.CMsgPracticeLobbySetDetails{
		GameName:        proto.String(s.lobbyName),
		PassKey:         proto.String(s.lobbyPass),
		GameMode:        &gameMode,
		Visibility:      &visibility,
		ServerRegion:    proto.Uint32(3), // Europe West
		AllowCheats:     proto.Bool(false),
		AllowSpectating: proto.Bool(true),
	}

	var lobbyErr error
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		lobbyErr = s.dota.LeaveCreateLobby(ctx, details, true)
		cancel()
		if lobbyErr == nil {
			break
		}
		log.Printf("[bot] create lobby attempt %d/3: %v", attempt, lobbyErr)
		if attempt < 3 {
			time.Sleep(3 * time.Second)
		}
	}
	if lobbyErr != nil {
		log.Printf("[bot] create lobby failed after 3 attempts: %v", lobbyErr)
		return
	}
	log.Printf("[bot] lobby created: %q (pass: %q)", s.lobbyName, s.lobbyPass)

	// Kick the bot out of its team slot so it doesn't occupy a player slot.
	// The bot retains host status and stays in the unassigned pool.
	botID := s.botAccountID.Load()
	s.dota.KickLobbyMemberFromTeam(botID)
	log.Printf("[bot] kicked self from team slot (accountID=%d)", botID)

	for _, p := range players {
		sid, err := parseSteamID(p.SteamID)
		if err != nil {
			log.Printf("[bot] invalid steam ID for %s (%s): %v", p.DisplayName, p.SteamID, err)
			continue
		}
		s.dota.InviteLobbyMember(sid)
		log.Printf("[bot] invited %s (%s) to lobby", p.DisplayName, p.SteamID)
	}

	// Create a fresh channel for this lobby's !start signal.
	ch := make(chan struct{}, 1)
	s.startMu.Lock()
	s.startCh = ch
	s.startMu.Unlock()

	go s.waitForStart(ch)
}

// waitForStart blocks until !start is received in lobby chat, then opens the
// match gate and launches the lobby. Exits on timeout (4 hours).
func (s *Service) waitForStart(startCh chan struct{}) {
	log.Println("[bot] waiting for !start in lobby chat...")
	timeout := time.After(4 * time.Hour)

	select {
	case <-startCh:
		s.gate.Open()
		s.dota.LaunchLobby()
		log.Println("[bot] lobby launched — match gate open")

	case <-timeout:
		log.Println("[bot] !start not received within 4 hours — giving up")
	}

	// Clear the channel so stale !start signals don't affect the next lobby.
	s.startMu.Lock()
	s.startCh = nil
	s.startMu.Unlock()
}


func parseSteamID(s string) (steamid.SteamId, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return steamid.SteamId(id), nil
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
