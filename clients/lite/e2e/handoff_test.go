//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// A send between two computers: the receiver learns of it from its event
// stream, the sender of its delivery from its own.
func TestSendToDeviceAgainstRealServer(t *testing.T) {
	s := startServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sender := api.New(s.url, s.deviceLogin(t, "sender").Token)
	receiver := api.New(s.url, s.deviceLogin(t, "receiver").Token)

	listen := func(client *api.Client) <-chan [2]string {
		response, err := client.HandoffEvents(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		events := make(chan [2]string, 32)
		go func() {
			scanner, name := bufio.NewScanner(response.Body), ""
			for scanner.Scan() {
				if value, ok := strings.CutPrefix(scanner.Text(), "event: "); ok {
					name = value
				}
				if value, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
					events <- [2]string{name, value}
				}
			}
		}()
		return events
	}
	await := func(events <-chan [2]string, name, contains string) string {
		t.Helper()
		for {
			select {
			case event := <-events:
				if event[0] == name && strings.Contains(event[1], contains) {
					return event[1]
				}
			case <-ctx.Done():
				t.Fatalf("no %s event with %q", name, contains)
			}
		}
	}

	incoming := listen(receiver)
	await(incoming, "handoff:devices", "{}")
	outgoing := listen(sender)
	devices, err := sender.HandoffDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var target string
	for _, device := range devices {
		if device.Name == "receiver" && device.Target() {
			target = device.ID
		}
	}
	if target == "" {
		t.Fatalf("receiver is not a target: %+v", devices)
	}
	offer, err := sender.HandoffCreate(ctx, target, []api.HandoffNewFile{{Name: "hello.txt", Size: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sender.HandoffUpload(ctx, offer.ID, offer.Files[0].ID, 5, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	var arrived api.HandoffOffer
	if err = json.Unmarshal([]byte(await(incoming, "handoff:offer", offer.ID)), &arrived); err != nil || len(arrived.Files) != 1 {
		t.Fatalf("arrived=%+v %v", arrived, err)
	}
	if err = receiver.HandoffAction(ctx, offer.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	await(outgoing, "handoff:updated", `"status":"accepted"`)
	content, err := receiver.HandoffFileContent(ctx, offer.ID, arrived.Files[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer content.Body.Close()
	if data, err := io.ReadAll(content.Body); err != nil || string(data) != "hello" {
		t.Fatalf("content=%q %v", data, err)
	}
	await(outgoing, "handoff:updated", `"status":"delivered"`)
	if err = receiver.HandoffAction(ctx, offer.ID, "complete"); err != nil {
		t.Fatal(err)
	}
}
