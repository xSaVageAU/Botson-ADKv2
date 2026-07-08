package discord

import (
	"testing"

	"google.golang.org/adk/v2/cmd/launcher"
)

func TestNewGateway(t *testing.T) {
	config := &launcher.Config{}

	// Should initialize the struct correctly (but not start connection without token)
	gw, err := New("dummy_token", config)
	if err != nil {
		t.Fatalf("failed to construct gateway: %v", err)
	}

	if gw.session == nil {
		t.Error("expected session to be initialized, got nil")
	}

	if gw.activeSessions == nil {
		t.Error("expected activeSessions map to be initialized, got nil")
	}
}

func TestGetChannelName(t *testing.T) {
	gw := &Gateway{}
	// Test fallback behavior if discord session is nil / not connected
	name := gw.getChannelName(nil, "12345", nil)
	if name != "12345" {
		t.Errorf("expected channelID fallback '12345', got %q", name)
	}
}
