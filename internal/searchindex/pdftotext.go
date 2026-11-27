package searchindex

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var pdftotextPath = sync.OnceValue(func() string {
	path, err := exec.LookPath("pdftotext")
	if err != nil {
		return ""
	}
	return path
})

// runPdftotext reads the PDF from stdin; nothing is written to disk.
func runPdftotext(ctx context.Context, binary string, source []byte) (string, error) {
	command := exec.CommandContext(ctx, binary, "-q", "-enc", "UTF-8", "-", "-")
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	command.Stdin = bytes.NewReader(source)
	command.WaitDelay = time.Second
	out := &cappedOutput{limit: 2 * MaxTextBytes}
	command.Stdout = out
	if err := command.Run(); err != nil {
		return "", err
	}
	return strings.ToValidUTF8(out.buffer.String(), ""), nil
}

// cappedOutput keeps the first limit bytes and lets the rest drain.
type cappedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (c *cappedOutput) Write(p []byte) (int, error) {
	c.buffer.Write(p[:min(len(p), max(0, c.limit-c.buffer.Len()))])
	return len(p), nil
}
