// Package bot wraps the Steam + Dota2 clients and exposes a service that the
// HTTP server can use to create lobbies and invite players.
package bot

import (
	"context"
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
	"github.com/paralin/go-steam/steamid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"github.com/emilh/inhouse-e4/internal/db"
)

// Service maintains a persistent Steam + Dota2 GC connection and can create
// lobbies and invite players on demand.
type Service struct {
	username   string
	password   string
	lobbyName  string
	lobbyPass  string

	client *steam.Client
	dota   *dota2.Dota2

	gcReady     chan struct{}
	gcReadyOnce sync.Once

	botAccountID atomic.Uint32
}

// New reads credentials from the environment and returns a Service ready to
// be started. Call Start in a goroutine after construction.
func New() *Service {
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
		username:  username,
		password:  password,
		lobbyName: getEnvOr("LOBBY_NAME", "E4 Inhouse"),
		lobbyPass: getEnvOr("LOBBY_PASSWORD", "inhouse"),
		client:    client,
		dota:      d,
		gcReady:   make(chan struct{}),
	}
}

// Start connects to Steam and runs the event loop. It blocks until ctx is
// cancelled or the connection is permanently lost. Run it in a goroutine.
func (s *Service) Start(ctx context.Context) {
	go func() {
		for event := range s.client.Events() {
			switch e := event.(type) {

			case *steam.ConnectedEvent:
				log.Println("[bot] connected to Steam, logging in...")
				s.client.Auth.LogOn(&steam.LogOnDetails{
					Username: s.username,
					Password: s.password,
				})

			case *steam.LoggedOnEvent:
				log.Printf("[bot] logged in (steamID: %d)", e.ClientSteamId)
				s.botAccountID.Store(uint32(e.ClientSteamId))
				s.client.GC.SetGamesPlayed(uint64(dota2.AppID))
				s.dota.SayHello()

			case *events.GCConnectionStatusChanged:
				log.Printf("[bot] GC status: %v", e.NewState)
				if e.NewState == protocol.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION {
					s.gcReadyOnce.Do(func() { close(s.gcReady) })
				}

			case *steam.LogOnFailedEvent:
				log.Printf("[bot] login failed: %v — will retry on reconnect", e.Result)

			case *steam.DisconnectedEvent:
				log.Println("[bot] disconnected from Steam")

			case error:
				log.Printf("[bot] error: %v", e)
			}
		}
	}()

	addr := s.client.Connect()
	log.Printf("[bot] connecting to Steam at %v", addr)

	<-ctx.Done()
	log.Println("[bot] context cancelled, disconnecting")
	s.client.Disconnect()
}

// CreateLobbyAndInvite waits for the GC to be ready, creates a practice lobby,
// then sends a lobby invite to each player's Steam account. Designed to run in
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.dota.LeaveCreateLobby(ctx, details, true); err != nil {
		log.Printf("[bot] create lobby: %v", err)
		return
	}
	log.Printf("[bot] lobby created: %q (pass: %q)", s.lobbyName, s.lobbyPass)

	for _, p := range players {
		sid, err := parseSteamID(p.SteamID)
		if err != nil {
			log.Printf("[bot] invalid steam ID for %s (%s): %v", p.DisplayName, p.SteamID, err)
			continue
		}
		s.dota.InviteLobbyMember(sid)
		log.Printf("[bot] invited %s (%s) to lobby", p.DisplayName, p.SteamID)
	}
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
