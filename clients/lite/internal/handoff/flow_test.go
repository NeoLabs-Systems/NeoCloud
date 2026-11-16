package handoff

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// scriptedDialogs answers the flow's questions and records them.
type scriptedDialogs struct {
	mu      sync.Mutex
	pick    int
	confirm bool
	err     error
	told    []string
	asked   []string
	options []string
}

func (d *scriptedDialogs) Choose(_ context.Context, message string, labels []string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked, d.options = append(d.asked, message), labels
	return d.pick, d.err
}

func (d *scriptedDialogs) Confirm(_ context.Context, message, ok string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, message)
	return d.confirm, d.err
}

func (d *scriptedDialogs) Inform(_ context.Context, message string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.told = append(d.told, message)
	return d.err
}

func flowFor(env *sendEnv, d *scriptedDialogs, files ...string) *Flow {
	return &Flow{Sender: env.sender, Dialogs: d, Foreground: true,
		ChooseFiles: func(context.Context) ([]string, bool) { return files, len(files) > 0 }}
}

func TestFlowOneDeviceConfirmsThenSends(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.set(func() { env.server.status = "delivered" })
	d := &scriptedDialogs{confirm: true}
	file := env.file(t, "a.txt", "hello")
	final, started := flowFor(env, d, file.Path).Run(context.Background(), nil)
	if !started || final.State != StateDelivered {
		t.Fatalf("%+v %v", final, started)
	}
	if len(d.asked) != 1 || !strings.Contains(d.asked[0], "Send a.txt (5 B) to Pixel 8?") {
		t.Errorf("asked: %q", d.asked)
	}
	if env.server.uploaded[101] != "hello" {
		t.Errorf("uploads: %v", env.server.uploaded)
	}
}

func TestFlowDeclinedConfirmationSendsNothing(t *testing.T) {
	env := newSendEnv(t, true)
	d := &scriptedDialogs{confirm: false}
	file := env.file(t, "a.txt", "hello")
	if _, started := flowFor(env, d).Run(context.Background(), []string{file.Path}); started {
		t.Error("started")
	}
	if env.server.creates != 0 {
		t.Error("the person said no")
	}
}

func TestFlowSeveralDevicesUsesTheChosenOne(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.devices = append(env.server.devices, api.HandoffDevice{ID: "s:9", Name: "Firefox", Kind: "browser", CanReceive: true, Online: true})
	env.server.set(func() { env.server.status = "delivered" })
	d := &scriptedDialogs{pick: 1}
	file := env.file(t, "a.txt", "hello")
	final, _ := flowFor(env, d).Run(context.Background(), []string{file.Path})
	if final.To != "Firefox" || len(d.options) != 2 || d.options[1] != "Firefox (browser)" {
		t.Errorf("final %+v options %q", final, d.options)
	}
	// Closing the list sends nothing.
	d2 := &scriptedDialogs{err: ErrCancelled}
	if _, started := flowFor(env, d2).Run(context.Background(), []string{file.Path}); started {
		t.Error("cancelled list started a send")
	}
}

func TestFlowTellsWhenNobodyIsOnline(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.devices = nil
	d := &scriptedDialogs{}
	file := env.file(t, "a.txt", "x")
	if _, started := flowFor(env, d).Run(context.Background(), []string{file.Path}); started {
		t.Error("started")
	}
	if len(d.told) != 1 || d.told[0] != NoDeviceMessage || !strings.Contains(NoDeviceMessage, "No other device is online right now") {
		t.Errorf("told: %q", d.told)
	}
}

func TestFlowRefusesFoldersAndFallsBackToANotification(t *testing.T) {
	env := newSendEnv(t, true)
	d := &scriptedDialogs{err: ErrNoDialog}
	if _, started := flowFor(env, d).Run(context.Background(), []string{env.dir}); started {
		t.Error("started")
	}
	if n := env.notices(); len(n) != 1 || !strings.Contains(n[0], "is a folder") {
		t.Errorf("notices: %q", n)
	}
	if env.server.creates != 0 {
		t.Error("nothing is sent for a folder")
	}
}

func TestFlowWithoutADialogSendsToTheOnlyDevice(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.set(func() { env.server.status = "delivered" })
	d := &scriptedDialogs{err: ErrNoDialog}
	file := env.file(t, "a.txt", "x")
	final, started := flowFor(env, d).Run(context.Background(), []string{file.Path})
	if !started || final.State != StateDelivered {
		t.Errorf("%+v %v", final, started)
	}
	// With several devices and no dialog there is nobody to ask.
	env.server.devices = append(env.server.devices, api.HandoffDevice{ID: "s:9", Name: "Firefox", CanReceive: true, Online: true})
	if _, started := flowFor(env, d).Run(context.Background(), []string{file.Path}); started {
		t.Error("it must not guess which device")
	}
	if n := env.notices(); !strings.Contains(strings.Join(n, "\n"), "send --to") {
		t.Errorf("notices: %q", n)
	}
}

func TestFlowCancelledFileDialogDoesNothing(t *testing.T) {
	env := newSendEnv(t, true)
	if _, started := flowFor(env, &scriptedDialogs{}).Run(context.Background(), nil); started {
		t.Error("started")
	}
	if env.server.creates != 0 {
		t.Error("created")
	}
}
