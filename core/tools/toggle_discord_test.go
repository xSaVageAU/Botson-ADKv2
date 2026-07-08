package tools

import "testing"

// TestToggleDiscord exercises the tool's own arg-validation and error
// propagation. It deliberately never reaches a real Discord connection --
// core/interface/discord's own singleton_test.go covers that layer -- this
// just confirms ToggleDiscord forwards to it and surfaces its errors rather
// than swallowing them.
func TestToggleDiscord(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("rejects an invalid action", func(t *testing.T) {
		ctx := newFakeContext()
		if _, err := ToggleDiscord(ctx, ToggleDiscordArgs{Action: "pause"}); err == nil {
			t.Fatal("expected an error for an invalid action, got none")
		}
	})

	t.Run("stop is a no-op when nothing is running", func(t *testing.T) {
		ctx := newFakeContext()
		result, err := ToggleDiscord(ctx, ToggleDiscordArgs{Action: "stop"})
		if err != nil {
			t.Fatalf("ToggleDiscord stop failed: %v", err)
		}
		if result.Running {
			t.Fatal("expected Running=false after stop")
		}
	})

	t.Run("start surfaces the underlying gateway error when the core isn't initialized", func(t *testing.T) {
		ctx := newFakeContext()
		if _, err := ToggleDiscord(ctx, ToggleDiscordArgs{Action: "start"}); err == nil {
			t.Fatal("expected an error starting Discord when discord.InitCore has not been called, got none")
		}
	})
}
