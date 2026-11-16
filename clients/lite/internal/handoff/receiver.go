package handoff

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

const (
	maxSeen        = 500
	maxBackoff     = 30 * time.Second
	settingPoll    = 2 * time.Second
	maxListedNames = 3
)

// Receiver listens for sends to this computer and saves them.
type Receiver struct {
	Config *config.Store
	Logger *log.Logger
	// Prompt asks what to do with a send; Notify shows a desktop notice.
	Prompt Prompter
	Notify func(title, body string)
	// ChooseFolder shows the folder dialog (ok false: cancelled).
	ChooseFolder func(ctx context.Context) (path string, ok bool)
	// Downloads is the folder "Save to Downloads" uses.
	Downloads func() string
	// Updated is told about every change of a send (also one this computer
	// made), so a sender waiting for the answer need not wait for its next
	// poll; DevicesChanged when the list of devices changed.
	Updated        func(offerID string)
	DevicesChanged func()

	// client builds the API client for the signed-in account; tests replace it.
	client func(cfg config.Config) *api.Client
	wait   func(time.Duration)

	mu   sync.Mutex // one send at a time: one dialog at a time
	seen map[string]bool
	ord  []string
}

// New returns a receiver with the real dialogs wired in by the caller.
func New(cfg *config.Store, logger *log.Logger) *Receiver {
	return &Receiver{
		Config:    cfg,
		Logger:    logger,
		Prompt:    NewNativePrompt(),
		Downloads: defaultDownloads,
		client: func(c config.Config) *api.Client {
			client := api.New(c.Server, c.Token)
			return client
		},
		wait: time.Sleep,
		seen: map[string]bool{},
	}
}

func defaultDownloads() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

// Run keeps this computer online for the account's devices until ctx ends,
// reconnecting with backoff. Turning "Receive from my devices" on or off, or
// signing in or out, restarts the connection.
func (r *Receiver) Run(ctx context.Context) {
	failures := 0
	for ctx.Err() == nil {
		cfg := r.Config.Get()
		if !cfg.LoggedIn() || cfg.Token == "" {
			r.sleep(ctx, settingPoll)
			continue
		}
		err := r.connect(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failures++
			r.Logger.Printf("send to device: %v", err)
		} else {
			failures = 0
		}
		delay := time.Duration(1<<min(failures, 5)) * time.Second
		r.sleep(ctx, min(delay, maxBackoff))
	}
}

func (r *Receiver) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// connect holds one event stream until it ends or the settings change.
func (r *Receiver) connect(ctx context.Context, cfg config.Config) error {
	receive := cfg.ReceiveEnabled()
	streamCtx, stop := context.WithCancel(ctx)
	defer stop()
	client := r.client(cfg)
	res, err := client.HandoffEvents(streamCtx, receive)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	go r.watchSettings(streamCtx, stop, cfg.Server, cfg.Token, receive)
	return readEvents(res.Body, func(event Event) {
		switch event.Name {
		case "handoff:updated":
			r.updated(event)
		case "handoff:devices":
			if r.DevicesChanged != nil {
				r.DevicesChanged()
			}
		}
		if !receive || event.Name != "handoff:offer" {
			return
		}
		var offer api.HandoffOffer
		if decode(event, &offer) != nil || offer.ID == "" {
			return
		}
		go r.handle(streamCtx, client, offer)
	})
}

func (r *Receiver) updated(event Event) {
	var payload struct {
		Offer api.HandoffOffer `json:"offer"`
	}
	if r.Updated != nil && decode(event, &payload) == nil && payload.Offer.ID != "" {
		r.Updated(payload.Offer.ID)
	}
}

// watchSettings ends the stream when what it was opened with changes.
func (r *Receiver) watchSettings(ctx context.Context, stop context.CancelFunc, server, token string, receive bool) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(settingPoll):
		}
		now := r.Config.Get()
		if now.Server != server || now.Token != token || now.ReceiveEnabled() != receive {
			stop()
			return
		}
	}
}

// firstTime is true once per offer id for as long as the app runs: the
// server pushes a pending offer again on every reconnect.
func (r *Receiver) firstTime(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[id] {
		return false
	}
	r.seen[id] = true
	r.ord = append(r.ord, id)
	if len(r.ord) > maxSeen {
		delete(r.seen, r.ord[0])
		r.ord = r.ord[1:]
	}
	return true
}

func (r *Receiver) handle(ctx context.Context, client *api.Client, offer api.HandoffOffer) {
	if len(offer.Files) == 0 || !r.firstTime(offer.ID) {
		return
	}
	r.notify("NeoCloud", fmt.Sprintf("%s sent you %s", safeLabel(offer.From.Name), describeFiles(offer)))
	r.mu.Lock() // one dialog at a time
	defer r.mu.Unlock()
	choice, err := r.Prompt.Ask(ctx, promptText(offer))
	if err != nil {
		r.Logger.Printf("send to device: %v", err)
	}
	var folder string
	switch choice {
	case ChoiceDecline:
		r.finish(client.HandoffAction(ctx, offer.ID, "decline"), "decline")
		return
	case ChoiceLater:
		return
	case ChoiceFolder:
		picked, ok := "", false
		if r.ChooseFolder != nil {
			picked, ok = r.ChooseFolder(ctx)
		}
		if !ok {
			return
		}
		folder = picked
	default:
		folder = r.Downloads()
	}
	r.save(ctx, client, offer, folder)
}

func (r *Receiver) finish(err error, what string) {
	if err != nil {
		r.Logger.Printf("send to device: %s: %v", what, err)
	}
}

// save downloads every file of the offer into folder, then confirms.
func (r *Receiver) save(ctx context.Context, client *api.Client, offer api.HandoffOffer, folder string) {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		r.notify("Could not save", err.Error())
		return
	}
	if err := client.HandoffAction(ctx, offer.ID, "accept"); err != nil {
		r.notify("Could not save", friendly(err))
		return
	}
	for _, file := range offer.Files {
		final, err := Reserve(folder, file.Name)
		if err == nil {
			err = fetchFile(ctx, client.HandoffFileContent, offer.ID, file, final, func(int64) {}, r.wait)
		}
		if err != nil {
			r.Logger.Printf("send to device: %s: %v", file.Name, err)
			if final != "" {
				os.Remove(final)
			}
			r.notify("Could not save "+SafeName(file.Name), friendly(err))
			return
		}
	}
	r.finish(client.HandoffAction(ctx, offer.ID, "complete"), "complete")
	r.notify("Saved", fmt.Sprintf("%s saved to %s", describeFiles(offer), filepath.Base(folder)))
}

func (r *Receiver) notify(title, body string) {
	if r.Notify != nil {
		r.Notify(title, body)
	}
}

func friendly(err error) string {
	switch {
	case api.HasCode(err, "OFFER_EXPIRED"):
		return "The send expired before it could be saved."
	case api.HasCode(err, "OFFER_CLOSED"):
		return "The sender cancelled this send."
	}
	return "Something went wrong. Ask to send it again."
}

// safeLabel is a device name fit for a notification or dialog line.
func safeLabel(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return -1
		}
		return r
	}, name)
	if strings.TrimSpace(clean) == "" {
		return "Another device"
	}
	return clean
}

func describeFiles(offer api.HandoffOffer) string {
	if len(offer.Files) == 1 {
		return SafeName(offer.Files[0].Name)
	}
	return fmt.Sprintf("%d files", len(offer.Files))
}

func promptText(offer api.HandoffOffer) string {
	var names []string
	for i, file := range offer.Files {
		if i == maxListedNames {
			names = append(names, fmt.Sprintf("and %d more", len(offer.Files)-maxListedNames))
			break
		}
		names = append(names, SafeName(file.Name))
	}
	return fmt.Sprintf("%s wants to send you %s (%s):\n%s", safeLabel(offer.From.Name), describeFiles(offer),
		humanSize(offer.TotalSize), strings.Join(names, "\n"))
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
