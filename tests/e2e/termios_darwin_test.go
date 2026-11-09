package e2e

import (
	"os"

	"golang.org/x/sys/unix"
)

func terminalState(file *os.File) (*unix.Termios, error) {
	return unix.IoctlGetTermios(int(file.Fd()), unix.TIOCGETA)
}
