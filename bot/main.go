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
	"strings"
	"time"

	dota2 "github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/cso"
	"github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	steam "github.com/paralin/go-steam"
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

	username := mustEnv("STEAM_ACCOUNT_NAME")
	password := mustEnv("STEAM_PASSWORD")
	totpSecret := os.Getenv("STEAM_TOTP_SECRET") // set after steamguard-cli setup
	emailCode := os.Getenv("STEAM_AUTH_CODE")    // fallback for initial setup
	lobbyName := getEnvOr("LOBBY_NAME", "inhouse-poc")
	lobbyPass := getEnvOr("LOBBY_PASSWORD", "test123")

	// Resolve which auth method to use.
	// Priority: TOTP (permanent) > email code (one-time) > nothing (will fail).
	var twoFactorCode, authCode string
	if totpSecret != "" {
		code, err := generateSteamCode(totpSecret)
		if err != nil {
			log.Fatalf("generate Steam Guard code: %v", err)
		}
		twoFactorCode = code
		log.Printf("[steam] using TOTP auth code: %s", twoFactorCode)
	} else if emailCode != "" {
		authCode = emailCode
		log.Printf("[steam] using email auth code: %s", authCode)
	} else {
		log.Println("[steam] no auth method set — login will likely fail")
		log.Println("[steam] set STEAM_TOTP_SECRET (preferred) or STEAM_AUTH_CODE in .env")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	client := steam.NewClient()
	d := dota2.New(client, logger)

	gcReady := make(chan struct{})
	gcReadyOnce := make(chan struct{}, 1)

	go func() {
		for event := range client.Events() {
			switch e := event.(type) {

			case *steam.ConnectedEvent:
				log.Println("[steam] connected, logging in...")
				client.Auth.LogOn(&steam.LogOnDetails{
					Username:               username,
					Password:               password,
					TwoFactorCode:          twoFactorCode, // TOTP (mobile authenticator)
					AuthCode:               authCode,      // email Steam Guard (one-time)
					ShouldRememberPassword: true,
				})

			case *steam.LoggedOnEvent:
				log.Printf("[steam] logged in (steamID: %d)", e.ClientSteamId)
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

			case *steam.LogOnFailedEvent:
				log.Fatalf("[steam] login failed: %v", e.Result)

			case *steam.DisconnectedEvent:
				log.Println("[steam] disconnected")

			case error:
				log.Printf("[steam] error: %v", e)
			}
		}
	}()

	addr := client.Connect()
	log.Printf("[steam] connecting to %v...", addr)

	log.Println("waiting for Dota2 GC connection...")
	select {
	case <-gcReady:
		log.Println("[dota2] GC ready")
	case <-time.After(60 * time.Second):
		log.Fatal("timed out waiting for Dota2 GC")
	}

	// --- Create the lobby ---

	gameMode := uint32(protocol.DOTA_GameMode_DOTA_GAMEMODE_CM)
	visibility := protocol.DOTALobbyVisibility_DOTALobbyVisibility_Public
	details := &protocol.CMsgPracticeLobbySetDetails{
		GameName:   proto.String(lobbyName),
		PassKey:    proto.String(lobbyPass),
		GameMode:   &gameMode,
		Visibility: &visibility,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := d.LeaveCreateLobby(ctx, details, true); err != nil {
		cancel()
		log.Fatalf("create lobby: %v", err)
	}
	cancel()

	log.Printf("[lobby] created — name: %q  password: %q", lobbyName, lobbyPass)
	log.Println("[lobby] join via Dota 2 → Play → Custom Lobbies → search for the lobby name")
	log.Println("[lobby] play a game to completion, then the bot will fetch match details")

	// --- Watch for match completion via SOCache ---

	lobbyCtr, err := d.GetCache().GetContainerForTypeID(uint32(cso.Lobby))
	if err != nil {
		log.Fatalf("get lobby cache container: %v", err)
	}

	eventCh, unsub, err := lobbyCtr.Subscribe()
	if err != nil {
		log.Fatalf("subscribe to lobby cache: %v", err)
	}
	defer unsub()

	var matchID uint64
	log.Println("[lobby] subscribed to lobby state updates, waiting for match to end...")

	for {
		select {
		case <-eventCh:
			obj := lobbyCtr.GetOne()
			if obj == nil {
				continue
			}
			lobby, ok := obj.(*protocol.CSODOTALobby)
			if !ok {
				continue
			}
			state := lobby.GetState()
			mid := lobby.GetMatchId()
			log.Printf("[lobby] state=%v  match_id=%d", state, mid)

			if state == protocol.CSODOTALobby_POSTGAME && mid > 0 {
				matchID = mid
				log.Printf("[lobby] match finished! match_id=%d", matchID)
				goto fetchDetails
			}

		case <-time.After(4 * time.Hour):
			log.Fatal("timed out waiting for match to finish")
		}
	}

fetchDetails:
	log.Printf("[match] requesting details for match %d from GC...", matchID)

	req := &protocol.CMsgGCMatchDetailsRequest{
		MatchId: proto.Uint64(matchID),
	}
	resp := &protocol.CMsgGCMatchDetailsResponse{}

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer reqCancel()

	err = d.MakeRequest(
		reqCtx,
		uint32(protocol.EDOTAGCMsg_k_EMsgGCMatchDetailsRequest),
		req,
		uint32(protocol.EDOTAGCMsg_k_EMsgGCMatchDetailsResponse),
		resp,
	)
	if err != nil {
		log.Printf("[match] request failed: %v", err)
		log.Println("[match] RESULT: GC did not return match details for this lobby type")
		os.Exit(1)
	}

	raw, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		log.Fatalf("marshal response: %v", err)
	}

	fmt.Println("\n=== CMsgGCMatchDetailsResponse ===")
	fmt.Println(string(raw))
	fmt.Println("===================================")

	if resp.GetResult() != 1 {
		log.Printf("[match] RESULT: GC returned code %d (not OK) — no stats for this lobby type", resp.GetResult())
	} else {
		log.Println("[match] RESULT: SUCCESS — GC returned match details!")
		if match := resp.GetMatch(); match != nil {
			log.Printf("[match] duration=%ds  outcome=%v  players=%d",
				match.GetDuration(), match.GetMatchOutcome(), len(match.GetPlayers()))
		}
	}
}
