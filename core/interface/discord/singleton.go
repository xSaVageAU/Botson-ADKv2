package discord

import (
	"fmt"
	"sync"

	"botsonv2/core/config"

	"google.golang.org/adk/v2/cmd/launcher"
)

// coreConfig/active hold the shared, in-process Discord gateway lifecycle
// for whichever process is acting as Botson's core (today, `botson web`).
// Starting/stopping Discord is a plain function call spinning a goroutine
// + discordgo session up or down within that same process -- no separate
// OS process, no daemon id -- since the gateway already only depends on
// the *launcher.Config the core already holds in memory. Mirrors
// core/config.go's own package-level cached-singleton pattern.
var (
	mu         sync.Mutex
	active     *Gateway
	coreConfig *launcher.Config
)

// InitCore lets the core process register the shared launcher.Config once
// at startup, so StartGateway below doesn't need it passed in on every
// call. Called once by runWeb's bootstrap.
func InitCore(cfg *launcher.Config) {
	mu.Lock()
	defer mu.Unlock()
	coreConfig = cfg
}

// StartGateway starts the Discord gateway in-process. Returns an error if
// it's already running, the core hasn't been initialized yet, or no
// Discord token is configured.
func StartGateway() error {
	mu.Lock()
	defer mu.Unlock()

	if active != nil {
		return fmt.Errorf("Discord gateway is already running")
	}
	if coreConfig == nil {
		return fmt.Errorf("Discord gateway cannot start: core is not initialized")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if cfg.Discord.Token == "" {
		return fmt.Errorf("discord.token is not defined in ~/.botsonv2/config.json")
	}

	g, err := New(cfg.Discord.Token, coreConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize Discord gateway: %w", err)
	}
	if err := g.Start(); err != nil {
		return fmt.Errorf("failed to start Discord gateway: %w", err)
	}

	active = g
	return nil
}

// StopGateway stops the in-process Discord gateway. Stopping an
// already-stopped gateway is not an error, matching core/daemon.Stop's
// own contract.
func StopGateway() error {
	mu.Lock()
	defer mu.Unlock()

	if active == nil {
		return nil
	}
	err := active.Close()
	active = nil
	return err
}

// GatewayStatus reports whether the in-process Discord gateway is
// currently running.
func GatewayStatus() bool {
	mu.Lock()
	defer mu.Unlock()
	return active != nil
}
