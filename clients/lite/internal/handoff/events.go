package handoff

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// Event is one server-sent event.
type Event struct {
	Name string
	Data []byte
}

// readEvents parses a text/event-stream body and calls fn for every event
// until the stream ends. Comment lines (heartbeats) and the retry hint are
// skipped. A single event is capped so a misbehaving server cannot grow
// memory without bound.
func readEvents(body io.Reader, fn func(Event)) error {
	const maxLine = 1 << 20
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLine)
	var name string
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if name != "" && len(data) > 0 {
				fn(Event{Name: name, Data: []byte(strings.Join(data, "\n"))})
			}
			name, data = "", nil
		case strings.HasPrefix(line, ":"), strings.HasPrefix(line, "retry:"):
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return scanner.Err()
}

func decode(event Event, into any) error { return json.Unmarshal(event.Data, into) }
