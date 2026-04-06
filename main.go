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
	logger.SetLevel(logrus.DebugLevel)

	client := steam.NewClient()
	d := dota2.New(client, logger)

	gcReady := make(chan struct{})
	gcReadyOnce := make(chan struct{}, 1)

	// startCh is closed when "!start" is received in lobby chat.
	startCh := make(chan struct{})
	var startOnce sync.Once

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
				log.Fatalf("[steam] login failed: %v", e.Result)

			case *steam.DisconnectedEvent:
				log.Println("[steam] disconnected")

			case error:
				log.Printf("[steam] error: %v", e)
			}
		}
	}()

	/*
		type PortAddr struct {
			IP   net.IP
			Port uint16
		}

		162.254.199.181:27017
	*/

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
		AllowCheats: proto.Bool(true),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := d.LeaveCreateLobby(ctx, details, true); err != nil {
		cancel()
		log.Fatalf("create lobby: %v", err)
	}
	cancel()

	log.Printf("[lobby] created — name: %q  password: %q", lobbyName, lobbyPass)

	// --- Watch for match completion via SOCache ---

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
			log.Println("[lobby] Warning: Cache object found but could not be cast to CSODOTALobby")
		}
	} else {
		log.Println("[lobby] Warning: Lobby not found in SOCache after creation")
	}

	// --- Leave the player slot into the unassigned pool ---
	// Equivalent to clicking the red X next to a player name in the lobby UI.

	log.Println("[lobby] leaving player slot (moving to unassigned pool)...")
	d.JoinLobbyTeam(protocol.DOTA_GC_TEAM_DOTA_GC_TEAM_SPECTATOR, 0)

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

	log.Println("[lobby] join via Dota 2 → Play → Custom Lobbies → search for the lobby name")
	log.Println("[lobby] type !start in lobby chat when ready — the bot will launch the game")

	// --- Wait for !start command, then launch ---

	select {
	case <-startCh:
		log.Println("[lobby] launching game...")
		d.LaunchLobby()
		// Leave immediately — SOCache subscription survives the leave (confirmed
		// by prior test run) so we still get POSTGAME / match_id updates.
		d.LeaveLobby()
		log.Println("[lobby] left lobby after launch")
	case <-time.After(4 * time.Hour):
		log.Fatal("timed out waiting for !start command")
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
