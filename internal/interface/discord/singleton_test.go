package discord

import (
	"testing"

	"google.golang.org/adk/v2/cmd/launcher"
)

// TestSingletonGateway exercises StartGateway/StopGateway/GatewayStatus'
// error-path contract without ever actually dialing Discord's real gateway
// (Gateway.Start only does that once discordgo.Session.Open is reached,
// which none of these paths get to -- they're all rejected earlier, by
// singleton.go itself, mirroring daemon.Stop's own "stopping what's not
// running is a no-op" contract).
func TestSingletonGateway(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("status is false before anything starts", func(t *testing.T) {
		if GatewayStatus() {
			t.Fatal("expected GatewayStatus to be false before any StartGateway call")
		}
	})

	t.Run("stop is a no-op when nothing is running", func(t *testing.T) {
		if err := StopGateway(); err != nil {
			t.Fatalf("StopGateway with nothing active should be a no-op, got: %v", err)
		}
	})

	t.Run("start fails when the core has not called InitCore", func(t *testing.T) {
		if err := StartGateway(); err == nil {
			t.Fatal("expected an error starting before InitCore, got none")
		}
	})

	t.Run("start fails when no discord token is configured", func(t *testing.T) {
		InitCore(&launcher.Config{})
		defer InitCore(nil)

		if err := StartGateway(); err == nil {
			t.Fatal("expected an error starting with no discord.token configured, got none")
		}
		if GatewayStatus() {
			t.Fatal("GatewayStatus should remain false after a failed start")
		}
	})
}
