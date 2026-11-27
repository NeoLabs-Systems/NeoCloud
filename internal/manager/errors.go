package manager

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
)

const (
	codeInput    = "CLI_INPUT"
	codeNotReady = "SERVER_NOT_READY"
	// codeReported is a failure whose explanation is already on screen.
	codeReported = "CLI_REPORTED"
)

// inputError is a mistake in what was typed, not a problem with NeoCloud.
func inputError(message string) error { return cli.Errorf(codeInput, "%s", message) }

func notReady(port int) error {
	return cli.Errorf(codeNotReady, "NeoCloud did not answer on port %d.", port)
}

// guidance is what a failed command means to the person who ran it.
var guidance = map[string]struct{ title, next string }{
	codeInput:    {"That command didn't work", "neocloud help"},
	codeNotReady: {"NeoCloud started but isn't answering", "neocloud logs"},
}

// PrintError writes a failed command: a headline, what happened and what to try
// next.
func PrintError(w io.Writer, err error, args []string) {
	code := cli.CodeOf(err, "")
	if code == codeReported {
		return
	}
	u := cli.NewUI(false)
	u.Out, u.Err = w, w
	write := func(line string) { fmt.Fprintln(w, line) }
	if errors.Is(err, cli.ErrCancelled) {
		write("\n  " + u.Muted("Cancelled."))
		return
	}
	g, known := guidance[code]
	if !known {
		g.title, g.next = "Something went wrong", "neocloud doctor"
	}
	write("")
	write("  " + u.Error("✕") + " " + u.Bold(g.title))
	for _, line := range strings.Split(err.Error(), "\n") {
		write("    " + line)
	}
	write("")
	if code != codeInput {
		write("  " + u.Muted("Try") + " " + u.Command(g.next) + u.Muted("  ·  still stuck? neocloud repair tries the usual fixes"))
		if slices.Contains(args, "--details") && code != "" {
			write("  " + u.Muted("Code: "+code))
		}
		write("")
	}
}
