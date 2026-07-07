// Package setup implements the botson-full `setup install/uninstall/reset`
// commands: getting the binary onto the machine (and PATH), tearing it back
// off, and resetting config/data to a clean slate. Plain functions, no Cobra
// awareness, so cmd/botson-full stays a thin wrapper -- same shape as
// core/daemon and core/management.
package setup

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

var stdin = bufio.NewReader(os.Stdin)

// ReadLine prompts for a line of plain text. An empty answer returns
// defaultVal (which may itself be empty, meaning "no default").
func ReadLine(label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}

	line, err := stdin.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// ReadMasked prompts for a line of input without echoing it to the
// terminal, for secrets (API keys, bot tokens). Falls back to a plain,
// visible read if the input isn't a real terminal (piped/redirected
// stdin) rather than failing outright, since raw mode has nothing to
// attach to in that case.
func ReadMasked(label string) (string, error) {
	fmt.Printf("%s: ", label)
	if data, err := term.ReadPassword(int(os.Stdin.Fd())); err == nil {
		fmt.Println()
		return strings.TrimSpace(string(data)), nil
	}

	line, err := stdin.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// AskYesNo prompts a yes/no question. An empty answer takes defaultYes.
func AskYesNo(label string, defaultYes bool) (bool, error) {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("%s [%s]: ", label, hint)

	line, err := stdin.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return defaultYes, nil
	}
	return line == "y" || line == "yes", nil
}
