// Package setup implements the `botson setup install/uninstall/reset`
// commands: getting the binary onto the machine (and PATH), tearing it back
// off, and resetting config/data to a clean slate. Plain functions, no Cobra
// awareness, so cmd/botson stays a thin wrapper -- same shape as core/daemon
// and core/management.
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

// ReadMasked prompts for a line of input, echoing a `*` per keystroke
// rather than the actual characters -- enough to see that typing or
// pasting actually registered, without revealing the secret itself.
// Falls back to a plain, visible read if the input isn't a real terminal
// (piped/redirected stdin), since raw mode has nothing to attach to in
// that case.
func ReadMasked(label string) (string, error) {
	fmt.Printf("%s: ", label)

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
	defer term.Restore(fd, oldState)

	// Raw mode disables the terminal's own newline translation, so use
	// explicit \r\n rather than relying on fmt.Println here.
	var buf []byte
	for {
		b, err := stdin.ReadByte()
		if err != nil {
			fmt.Print("\r\n")
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		switch b {
		case '\r', '\n':
			fmt.Print("\r\n")
			return strings.TrimSpace(string(buf)), nil
		case 3: // Ctrl+C
			fmt.Print("\r\n")
			return "", fmt.Errorf("input cancelled")
		case 127, 8: // Backspace/Delete
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Print("\b \b")
			}
		default:
			buf = append(buf, b)
			fmt.Print("*")
		}
	}
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
