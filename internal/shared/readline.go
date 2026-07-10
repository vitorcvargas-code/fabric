package shared

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadLine reads a single line from stdin with line editing.
// Enter (\r) submits. Ctrl+J or Shift+Enter (raw \n) inserts a newline.
// Backspace deletes the last character. Ctrl+C exits.
func ReadLine() string {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		var buf [256]byte
		n, _ := os.Stdin.Read(buf[:])
		return strings.TrimSpace(string(buf[:n]))
	}
	defer term.Restore(fd, oldState)

	var line []byte
	for {
		var b [1]byte
		_, err := os.Stdin.Read(b[:])
		if err != nil {
			break
		}
		switch b[0] {
		case '\r':
			fmt.Fprint(os.Stderr, "\r\n")
			return string(line)
		case '\n':
			line = append(line, '\n')
			fmt.Fprint(os.Stderr, "\r\n")
		case 0x7f:
			if len(line) > 0 {
				line = line[:len(line)-1]
				fmt.Fprint(os.Stderr, "\b \b")
			}
		case 0x03:
			fmt.Fprint(os.Stderr, "^C\r\n")
			os.Exit(1)
		default:
			if b[0] >= 32 {
				line = append(line, b[0])
				fmt.Fprint(os.Stderr, string(b[0]))
			}
		}
	}
	return string(line)
}
