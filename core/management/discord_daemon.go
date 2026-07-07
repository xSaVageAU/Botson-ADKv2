package management

import "botsonv2/core/daemon"

const discordDaemonID = "discord"
const discordDaemonDisplayName = "Discord gateway"

// discordDaemonChildArgs is the argv used to relaunch the current executable
// as the detached Discord gateway process.
var discordDaemonChildArgs = []string{"discord", "__daemon-child"}

// StartDiscordDaemon launches the Discord gateway as a detached background
// process, waiting for it to report itself ready.
func StartDiscordDaemon() (pid int, logPath string, err error) {
	return daemon.Start(discordDaemonID, discordDaemonDisplayName, discordDaemonChildArgs)
}

// StopDiscordDaemon asks the background Discord gateway to shut down
// gracefully, or force-kills it if force is true.
func StopDiscordDaemon(force bool) error {
	return daemon.Stop(discordDaemonID, discordDaemonDisplayName, force)
}

// DiscordDaemonStatus reports whether the background Discord gateway is
// currently running.
func DiscordDaemonStatus() (daemon.Status, error) {
	return daemon.GetStatus(discordDaemonID, discordDaemonDisplayName)
}
