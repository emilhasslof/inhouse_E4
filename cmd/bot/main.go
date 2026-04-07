// Inhouse Buddy PoC — Dota 2 lobby bot
//
// Goal: connect to Steam, create a Dota 2 lobby, wait for the match to end,
// then call RequestMatchDetails on the GC and dump the raw response.
// This tells us whether the GC returns player stats for custom lobby matches.
package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dota2 "github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/cso"
	"github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	steam "github.com/paralin/go-steam"
	"github.com/paralin/go-steam/protocol/steamlang"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

// steamGuardChars is Steam's custom TOTP alphabet.
var steamGuardChars = []byte("23456789BCDFGHJKMNPQRTVWXY")

// generateSteamCode computes a 5-character Steam Guard code from a base64-encoded
// shared_secret. Steam uses HMAC-SHA1 TOTP with its own character set.
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

// loadEnv reads KEY=VALUE pairs from a file into the process environment.
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var not set: %s", key)
	}
	return v
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	loadEnv(".env")
	log.Println("[bot] starting up...")

	username := mustEnv("STEAM_ACCOUNT_NAME")
	password := mustEnv("STEAM_PASSWORD")
	totpSecret := os.Getenv("STEAM_TOTP_SECRET") // set after steamguard-cli setup
	emailCode := os.Getenv("STEAM_AUTH_CODE")    // fallback for initial setup
	lobbyName := getEnvOr("LOBBY_NAME", "inhouse-poc")
	lobbyPass := getEnvOr("LOBBY_PASSWORD", "test123")

	if totpSecret == "" && emailCode == "" {
		log.Println("[steam] no auth method set — login will likely fail")
		log.Println("[steam] set STEAM_TOTP_SECRET (preferred) or STEAM_AUTH_CODE in .env")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	client := steam.NewClient()
	d := dota2.New(client, logger)

	// logOn generates a fresh TOTP code and calls LogOn. Generating it here
	// (rather than at startup) ensures the code is valid at the moment of use,
	// avoiding the ~80% failure rate caused by codes generated near the end of
	// their 30s window expiring before the TCP handshake to Steam completes.
	logOn := func() {
		var twoFactorCode, authCode string
		if totpSecret != "" {
			code, err := generateSteamCode(totpSecret)
			if err != nil {
				log.Fatalf("generate Steam Guard code: %v", err)
			}
			twoFactorCode = code
			log.Printf("[steam] logging in with fresh TOTP code: %s", twoFactorCode)
		} else if emailCode != "" {
			authCode = emailCode
			log.Printf("[steam] logging in with email auth code")
		}
		client.Auth.LogOn(&steam.LogOnDetails{
			Username:               username,
			Password:               password,
			TwoFactorCode:          twoFactorCode,
			AuthCode:               authCode,
			ShouldRememberPassword: true,
		})
	}

	gcReady := make(chan struct{})
	gcReadyOnce := make(chan struct{}, 1)

	// startCh is closed when "!start" is received in lobby chat or via stdin.
	startCh := make(chan struct{})
	var startOnce sync.Once

	// botAccountID is set once we receive LoggedOnEvent (lower 32 bits of SteamID).
	var botAccountID atomic.Uint32

	go func() {
		for event := range client.Events() {
			switch e := event.(type) {

			case *steam.ConnectedEvent:
				log.Println("[steam] connected, logging in...")
				logOn()

			case *steam.LoggedOnEvent:
				log.Printf("[steam] logged in (steamID: %d)", e.ClientSteamId)
				botAccountID.Store(uint32(e.ClientSteamId))
				client.GC.SetGamesPlayed(uint64(dota2.AppID))
				d.SayHello()

			case *events.GCConnectionStatusChanged:
				log.Printf("[dota2] GC status: %v", e.NewState)
				if e.NewState == protocol.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION {
					select {
					case gcReadyOnce <- struct{}{}:
						close(gcReady)
					default:
					}
				}

			case *events.ChatMessage:
				if strings.TrimSpace(e.GetText()) == "!start" {
					log.Printf("[chat] received !start from %s — launching game", e.GetPersonaName())
					startOnce.Do(func() { close(startCh) })
				}

			case *events.ReadyUpStatus:
				log.Printf("[readyup] lobby=%d accepted=%v declined=%v local_state=%v",
					e.GetLobbyId(), e.GetAcceptedIds(), e.GetDeclinedIds(), e.GetLocalReadyState())

			case *events.UnhandledGCPacket:
				msgType := e.Packet.MsgType
				name := protocol.EDOTAGCMsg_name[int32(msgType)]
				if name == "" {
					log.Printf("[gc] unhandled packet type=%d", msgType)
				} else {
					log.Printf("[gc] unhandled packet type=%d (%s)", msgType, name)
				}

			case *steam.LogOnFailedEvent:
				if e.Result == steamlang.EResult_TwoFactorCodeMismatch && totpSecret != "" {
					log.Printf("[steam] TOTP code mismatch (code expired mid-window) — waiting for next window and retrying...")
					// Wait until we're safely into a new 30s window before regenerating.
					remaining := time.Now().Unix() % 30
					time.Sleep(time.Duration(30-remaining+1) * time.Second)
					logOn()
				} else {
					log.Fatalf("[steam] login failed: %v", e.Result)
				}

			case *steam.DisconnectedEvent:
				log.Println("[steam] disconnected")

			case error:
				log.Printf("[steam] error: %v", e)
			}
		}
	}()

	// --- Stdin command handler ---
	// Commands:
	//   start  — trigger game launch (same as !start in lobby chat)
	//   leave  — leave the lobby
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			switch strings.TrimSpace(scanner.Text()) {
			case "start", "launch":
				log.Println("[cmd] manually triggering game launch")
				startOnce.Do(func() { close(startCh) })
			case "leave":
				log.Println("[cmd] leaving lobby")
				d.LeaveLobby()
			default:
				log.Println("[cmd] unknown command — try: start, leave")
			}
		}
	}()

	addr := client.Connect()
	log.Printf("[steam] connecting to %v", addr)

	log.Println("waiting for Dota2 GC connection...")
	select {
	case <-gcReady:
		log.Println("[dota2] GC ready")
	case <-time.After(60 * time.Second):
		log.Fatal("timed out waiting for Dota2 GC")
	}

	// --- Create the lobby ---

	// TODO: change back to DOTA_GAMEMODE_CM for real matches
	gameMode := uint32(protocol.DOTA_GameMode_DOTA_GAMEMODE_AP)
	visibility := protocol.DOTALobbyVisibility_DOTALobbyVisibility_Public
	details := &protocol.CMsgPracticeLobbySetDetails{
		GameName:     proto.String(lobbyName),
		PassKey:      proto.String(lobbyPass),
		GameMode:     &gameMode,
		Visibility:   &visibility,
		ServerRegion: proto.Uint32(3), // Europe West
		// TODO: remove cheats before running real matches
		AllowCheats:     proto.Bool(false),
		AllowSpectating: proto.Bool(true),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := d.LeaveCreateLobby(ctx, details, true); err != nil {
		cancel()
		log.Fatalf("create lobby: %v", err)
	}
	cancel()

	log.Printf("[lobby] created — name: %q  password: %q", lobbyName, lobbyPass)

	// Read lobby ID from SOCache so we can poll SourceTV by lobby ID later.
	lobbyCtr, err := d.GetCache().GetContainerForTypeID(uint32(cso.Lobby))
	if err != nil {
		log.Fatalf("get lobby cache container: %v", err)
	}

	// Give the cache a moment to synchronize the initial lobby state from the GC.
	time.Sleep(2 * time.Second)

	var lobbyID uint64
	if obj := lobbyCtr.GetOne(); obj != nil {
		if lobby, ok := obj.(*protocol.CSODOTALobby); ok {
			lobbyID = lobby.GetLobbyId()
			log.Printf("[lobby] VERIFIED — LobbyID: %d  State: %v  Region: %v",
				lobbyID, lobby.GetState(), lobby.GetServerRegion())
		} else {
			log.Fatalf("[lobby] cache object exists but could not be cast to CSODOTALobby")
		}
	} else {
		log.Fatalf("[lobby] lobby not found in SOCache after creation — cannot continue")
	}

	// --- Join lobby chat channel to receive commands ---

	if lobbyID != 0 {
		channelName := "Lobby_" + strconv.FormatUint(lobbyID, 10)
		chatCtx, chatCancel := context.WithTimeout(context.Background(), 10*time.Second)
		chatResp, chatErr := d.JoinChatChannel(chatCtx, channelName, protocol.DOTAChatChannelTypeT_DOTAChannelType_Lobby, false)
		chatCancel()
		if chatErr != nil {
			log.Printf("[chat] failed to join lobby chat %q: %v", channelName, chatErr)
		} else {
			log.Printf("[chat] joined lobby chat %q (channel_id=%d)", channelName, chatResp.GetChannelId())
		}
	} else {
		log.Println("[chat] skipping chat join — lobby ID unknown")
	}

	// --- Kick bot from its team slot so it doesn't block game launch ---
	// Kicking itself via KickLobbyMemberFromTeam moves the bot to the unassigned
	// player pool, where it remains in the lobby (and keeps host status) but is
	// not counted as a game client that needs to connect when the match starts.
	// JoinLobbyTeam with NOTEAM/BROADCASTER/SPECTATOR all still block — kick is
	// the only approach that works.
	log.Printf("[lobby] kicking self (accountID=%d) from team slot...", botAccountID.Load())
	d.KickLobbyMemberFromTeam(botAccountID.Load())

	log.Println("[lobby] type !start in lobby chat when ready — the bot will launch the game")

	// --- Wait for !start command, then launch ---

	select {
	case <-startCh:
		log.Println("[lobby] launching game...")
		d.LaunchLobby()
	case <-time.After(4 * time.Hour):
		log.Fatal("timed out waiting for !start command")
	}

	// --- Wait for match ID to appear in SOCache, then poll for completion ---
	// FindTopSourceTVGames only lists public games — private lobbies never appear.
	// Instead: read the match_id from the lobby SOCache (set by the GC shortly
	// after LaunchLobby), then poll RequestMatchDetails until it returns OK,
	// which is the definitive signal that the match is over and stats are ready.

	const (
		pollInterval = 30 * time.Second
		pollTimeout  = 4 * time.Hour
	)
	pollDeadline := time.Now().Add(pollTimeout)

	// Step 1: read match ID from SOCache. The GC populates this on the lobby
	// object within a few seconds of launch. Poll until we see it.
	log.Println("[match] waiting for GC to assign match ID to lobby...")
	var matchID uint64
	for matchID == 0 {
		time.Sleep(5 * time.Second)
		if time.Now().After(pollDeadline) {
			log.Fatalf("[match] gave up waiting for match ID after %s", pollTimeout)
		}
		obj := lobbyCtr.GetOne()
		if obj == nil {
			log.Println("[match] lobby not in SOCache yet, retrying...")
			continue
		}
		lobby, ok := obj.(*protocol.CSODOTALobby)
		if !ok {
			log.Fatalf("[match] SOCache object is not a CSODOTALobby")
		}
		mid := lobby.GetMatchId()
		state := lobby.GetState()
		log.Printf("[match] lobby state=%v  match_id=%d", state, mid)
		if mid > 0 {
			matchID = mid
		}
	}
	log.Printf("[match] got match ID %d — polling for completion every %s...", matchID, pollInterval)

	// Step 2: poll RequestMatchDetails until the GC returns a valid result.
	// The GC returns an error or result!=1 while the match is ongoing.
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	pollCount := 0

	for range ticker.C {
		if time.Now().After(pollDeadline) {
			log.Fatalf("[match] gave up after %s — match never completed", pollTimeout)
		}
		pollCount++

		reqCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		detailsResp := &protocol.CMsgGCMatchDetailsResponse{}
		reqErr := d.MakeRequest(
			reqCtx,
			uint32(protocol.EDOTAGCMsg_k_EMsgGCMatchDetailsRequest),
			&protocol.CMsgGCMatchDetailsRequest{MatchId: proto.Uint64(matchID)},
			uint32(protocol.EDOTAGCMsg_k_EMsgGCMatchDetailsResponse),
			detailsResp,
		)
		cancel()

		if reqErr != nil {
			log.Printf("[match] poll #%d: request error (match likely still in progress): %v", pollCount, reqErr)
			continue
		}

		result := detailsResp.GetResult()
		log.Printf("[match] poll #%d: GC result code=%d", pollCount, result)

		if result != 1 {
			log.Printf("[match] poll #%d: result not OK yet — match still in progress or stats not ready", pollCount)
			continue
		}

		// Success — print full response and summary.
		out, err := json.MarshalIndent(detailsResp, "", "  ")
		if err != nil {
			log.Fatalf("marshal response: %v", err)
		}
		fmt.Println("\n=== CMsgGCMatchDetailsResponse ===")
		fmt.Println(string(out))
		fmt.Println("===================================")

		match := detailsResp.GetMatch()
		if match != nil {
			log.Printf("[match] RESULT: SUCCESS — duration=%ds  outcome=%v  players=%d",
				match.GetDuration(), match.GetMatchOutcome(), len(match.GetPlayers()))
		} else {
			log.Println("[match] RESULT: SUCCESS — but match object is nil (unexpected)")
		}
		return
	}
}
