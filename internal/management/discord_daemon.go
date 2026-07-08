package management

import (
	"os"
	"time"

	"botsonv2/internal/daemon"
	"botsonv2/internal/interface/discord"
)

const discordDaemonDisplayName = "Discord gateway"

// discordStartedAt records when the in-process gateway was last started,
// purely for DiscordDaemonStatus's StartedAt field -- there's no separate
// process to read this back from once Discord runs in-process, so it's
// tracked here instead.
var discordStartedAt time.Time

// StartDiscordDaemon starts the Discord gateway in-process within the
// core (see internal/interface/discord.StartGateway) rather than spawning a
// separate OS process. The exported name/signature are unchanged from
// when this did spawn a process, so cmd/botson/cmd_discord.go and
// internal/interface/web/api_dashboard.go need no changes -- only the
// implementation underneath. pid/logPath are vestigial now (the gateway
// shares the core's own PID and logs interleave with the core's own
// stdout) but kept for backward compatibility with existing callers.
func StartDiscordDaemon() (pid int, logPath string, err error) {
	if err := discord.StartGateway(); err != nil {
		return 0, "", err
	}
	discordStartedAt = time.Now()
	return os.Getpid(), "", nil
}

// StopDiscordDaemon stops the in-process Discord gateway. force is
// accepted for backward compatibility with the old separate-process
// signature but has no effect -- stopping an in-process goroutine has no
// "graceful vs. force" distinction the way killing an OS process does.
func StopDiscordDaemon(force bool) error {
	return discord.StopGateway()
}

// DiscordDaemonStatus reports whether the in-process Discord gateway is
// currently running, in the same daemon.Status shape callers already
// expect (so JSON responses to the web console are unchanged).
func DiscordDaemonStatus() (daemon.Status, error) {
	if !discord.GatewayStatus() {
		return daemon.Status{ID: "discord", DisplayName: discordDaemonDisplayName}, nil
	}
	return daemon.Status{
		ID:          "discord",
		DisplayName: discordDaemonDisplayName,
		Running:     true,
		PID:         os.Getpid(),
		StartedAt:   discordStartedAt,
	}, nil
}
