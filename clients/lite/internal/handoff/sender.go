package handoff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	gosync "sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

const (
	defaultPoll  = 5 * time.Second
	keepFinished = 8
)

// Sender sends this computer's files to the account's other devices. It is
// independent of "Receive from my devices": that switch only decides whether
// other devices may send here.
type Sender struct {
	Config *config.Store
	Logger *log.Logger
	// Notify shows a desktop notice (nil: none). Each send tells the person
	// at most twice: when it starts (only with notifications set to
	// "Everything") and how it ended (unless notifications are off).
	Notify func(title, body string)

	// client builds the API client for the signed-in account; tests replace it.
	client func(cfg config.Config) *api.Client
	poll   time.Duration
	ctx    context.Context

	mu      gosync.Mutex
	entries []*entry // newest first
	wake    map[string]chan struct{}
}

type entry struct {
	t      Transfer
	cancel context.CancelFunc
}

// NewSender returns a sender whose background sends end with ctx.
func NewSender(ctx context.Context, cfg *config.Store, logger *log.Logger) *Sender {
	return &Sender{
		Config: cfg,
		Logger: logger,
		client: func(c config.Config) *api.Client {
			client := api.New(c.Server, c.Token)
			client.Up = api.NewLimiter(c.UploadKiBps * 1024)
			return client
		},
		poll: defaultPoll,
		ctx:  ctx,
		wake: map[string]chan struct{}{},
	}
}

func (s *Sender) logf(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, args...)
	}
}

// Options change how Send runs.
type Options struct {
	// NoWait returns once the files are uploaded instead of waiting for the
	// other device's answer.
	NoWait bool
	// Update gets a snapshot after every change (progress is throttled).
	Update func(Transfer)
}

// signedIn is the account's client, or the sentence to show when there is none.
func (s *Sender) signedIn() (*api.Client, error) {
	cfg := s.Config.Get()
	switch {
	case !cfg.LoggedIn():
		return nil, userErrorf("Sign in to NeoCloud first.")
	case cfg.Locked() || cfg.Token == "":
		return nil, userErrorf("NeoCloud is locked. Plug in your security key.")
	}
	return s.client(cfg), nil
}

// Devices are the other devices that can be sent to right now, as the server
// lists them: online, accepting files, not this computer.
func (s *Sender) Devices(ctx context.Context) ([]api.HandoffDevice, error) {
	client, err := s.signedIn()
	if err != nil {
		return nil, err
	}
	all, err := client.HandoffDevices(ctx)
	if err != nil {
		_, message := outcomeOf(ctx, err)
		return nil, userErrorf("%s", message)
	}
	var targets []api.HandoffDevice
	for _, device := range all {
		if device.Target() {
			targets = append(targets, device)
		}
	}
	return targets, nil
}

// Send runs one send to its end and returns how it ended.
func (s *Sender) Send(ctx context.Context, device api.HandoffDevice, items []Item, opt Options) Transfer {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return s.execute(ctx, s.add(cancel), device, items, opt)
}

// Start sends in the background and returns the send's id at once.
func (s *Sender) Start(device api.HandoffDevice, items []Item) string {
	ctx, cancel := context.WithCancel(s.ctx)
	id := s.add(cancel)
	go func() {
		defer cancel()
		s.execute(ctx, id, device, items, Options{})
	}()
	return id
}

func (s *Sender) execute(ctx context.Context, id string, device api.HandoffDevice, items []Item, opt Options) Transfer {
	defer removeTemporary(items)
	client, err := s.signedIn()
	if err != nil {
		t := Transfer{ID: id, To: device.Name, Label: Describe(items), State: StateFailed, Message: err.Error(), At: time.Now()}
		s.record(id, t)
		return t
	}
	level := s.Config.Get().NotifyLevel()
	if level == config.NotifyAll {
		s.notify("Sending "+Describe(items), "to "+safeLabel(device.Name))
	}
	final := s.run(ctx, client, request{device: device, items: items, noWait: opt.NoWait, update: func(t Transfer) {
		t.ID = id
		s.record(id, t)
		if opt.Update != nil {
			opt.Update(t)
		}
	}})
	if level != config.NotifyOff {
		if title, body := outcomeNotice(final); title != "" {
			s.notify(title, body)
		}
	}
	final.ID = id
	return final
}

func outcomeNotice(t Transfer) (title, body string) {
	to := safeLabel(t.To)
	switch t.State {
	case StateDelivered:
		return "Delivered to " + to, t.Label
	case StateDeclined:
		return "Declined", fmt.Sprintf("%s declined %s", to, t.Label)
	case StateExpired:
		return "Not received", fmt.Sprintf("%s did not receive %s in time", to, t.Label)
	case StateFailed:
		return "Could not send " + t.Label, t.Message
	}
	return "", ""
}

func (s *Sender) notify(title, body string) {
	if s.Notify != nil {
		s.Notify(title, body)
	}
}

func (s *Sender) add(cancel context.CancelFunc) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand never fails on supported systems
	}
	id := hex.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append([]*entry{{t: Transfer{ID: id, Active: true, At: time.Now()}, cancel: cancel}}, s.entries...)
	return id
}

func (s *Sender) record(id string, t Transfer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	finished := 0
	kept := s.entries[:0]
	for _, e := range s.entries {
		if e.t.ID == id {
			e.t = t
		}
		if !e.t.Active {
			if finished++; finished > keepFinished {
				continue
			}
		}
		kept = append(kept, e)
	}
	s.entries = kept
}

// Transfers are the running sends and the last few results, newest first.
func (s *Sender) Transfers() []Transfer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Transfer, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.t
	}
	return out
}

// Cancel stops a running send and cancels it on the server.
func (s *Sender) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.t.ID == id && e.t.Active {
			e.cancel()
			return true
		}
	}
	return false
}

// Dismiss forgets a finished send.
func (s *Sender) Dismiss(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	for _, e := range s.entries {
		if e.t.ID != id || e.t.Active {
			kept = append(kept, e)
		}
	}
	s.entries = kept
}

// Kick tells a send waiting for its answer that its offer changed (the
// event stream's "handoff:updated"), so it asks the server now.
func (s *Sender) Kick(offerID string) {
	s.mu.Lock()
	ch := s.wake[offerID]
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Sender) watch(offerID string) <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.wake[offerID] = ch
	s.mu.Unlock()
	return ch
}

func (s *Sender) unwatch(offerID string) {
	s.mu.Lock()
	delete(s.wake, offerID)
	s.mu.Unlock()
}

func removeTemporary(items []Item) {
	for _, item := range items {
		if item.Temporary {
			os.Remove(item.Path)
		}
	}
}
