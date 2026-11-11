package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/autostart"
	"github.com/neolabs/neocloud/clients/lite/internal/browser"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/qr"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	server := fs.String("server", "", "server address, e.g. https://cloud.example.com")
	noBrowser := fs.Bool("no-browser", false, "only print the approval link")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	a, err := openApp(stderrLogger(true))
	if err != nil {
		return err
	}
	defer a.close()
	if *server == "" {
		*server = a.cfg.Get().Server
	}
	if strings.TrimSpace(*server) == "" {
		return fmt.Errorf("--server is required")
	}
	if *server, err = api.CleanServer(*server); err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()
	client := api.New(*server, "")
	auth, err := client.Authorize(ctx, config.DefaultDeviceName(), "lite")
	if err != nil {
		return err
	}
	fmt.Printf("Approve this device in NeoCloud:\n  %s\nCode: %s\n", auth.VerificationURL, auth.UserCode)
	if code, err := qr.Terminal(auth.VerificationURL); err == nil {
		fmt.Printf("\nOr scan this with a phone that is signed in to NeoCloud:\n%s", code)
	}
	if !*noBrowser {
		_ = browser.Open(auth.VerificationURL)
	}
	token, err := client.WaitForToken(ctx, auth)
	if err != nil {
		return err
	}
	if err := a.engine.SignIn(*server, token); err != nil {
		return err
	}
	fmt.Printf("Signed in as %s.\n", token.User.Username)
	return nil
}

func cmdLogout([]string) error {
	a, err := openApp(stderrLogger(true))
	if err != nil {
		return err
	}
	defer a.close()
	if err := a.engine.SignOut(); err != nil {
		return err
	}
	fmt.Println("Signed out. Pairs are kept; sync state was reset.")
	return nil
}

func cmdPair(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	a, err := openApp(stderrLogger(true))
	if err != nil {
		return err
	}
	defer a.close()
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("pair add", flag.ContinueOnError)
		direction := fs.String("direction", config.Both, "both, up or down")
		var ignore stringList
		fs.Var(&ignore, "ignore", "glob to ignore (repeatable)")
		positional, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return err
		}
		if len(positional) != 2 {
			return errUsage
		}
		pair, err := config.NewPair(positional[0], positional[1], *direction, ignore)
		if err != nil {
			return err
		}
		if err := a.cfg.Update(func(c *config.Config) error { return c.AddPair(pair) }); err != nil {
			return err
		}
		fmt.Printf("Added %s: %s <-> /%s (%s)\n", pair.ID, pair.Local, pair.Remote, pair.Direction)
	case "list":
		for _, pair := range a.cfg.Get().Pairs {
			paused := ""
			if pair.Paused {
				paused = " [paused]"
			}
			fmt.Printf("%s  %s <-> /%s  %s%s\n", pair.ID, pair.Local, pair.Remote, pair.Direction, paused)
			if len(pair.Ignore) > 0 {
				fmt.Printf("    ignore: %s\n", strings.Join(pair.Ignore, ", "))
			}
		}
	case "remove":
		if len(args) != 2 {
			return errUsage
		}
		if err := a.engine.RemovePair(args[1]); err != nil {
			return err
		}
		fmt.Println("Removed. Files stay where they are.")
	case "confirm-deletes":
		if len(args) != 2 {
			return errUsage
		}
		confirmed, err := a.engine.ConfirmDeletes(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Confirmed: the next sync deletes %d file(s) on the server and %d on this computer (kept in .neocloud/trash).\n",
			confirmed.RemoteDeletes, confirmed.LocalDeletes)
	default:
		return errUsage
	}
	return nil
}

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.Bool("once", true, "sync every pair once and exit (the only mode)")
	quiet := fs.Bool("quiet", false, "do not log each transfer")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	a, err := openApp(stderrLogger(*quiet))
	if err != nil {
		return err
	}
	defer a.close()
	ctx, cancel := signalContext()
	defer cancel()
	if err := a.engine.SyncOnce(ctx); err != nil {
		return err
	}
	fmt.Println("Up to date.")
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the status as JSON")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	a, err := openApp(stderrLogger(true))
	if err != nil {
		return err
	}
	defer a.close()
	if *asJSON {
		return printStatusJSON(a)
	}
	cfg := a.cfg.Get()
	if !cfg.LoggedIn() {
		fmt.Println("Not signed in.")
	} else {
		fmt.Printf("Server:  %s\nAccount: %s\nDevice:  %s\n", cfg.Server, cfg.Username, cfg.DeviceName)
		client, _ := a.engine.Client()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if me, err := client.Me(ctx); err != nil {
			fmt.Printf("Token:   not accepted (%v)\n", err)
		} else if me.Quota.Limit > 0 {
			fmt.Printf("Storage: %s of %s\n", bytes(me.Quota.Used), bytes(me.Quota.Limit))
		} else {
			fmt.Printf("Storage: %s used\n", bytes(me.Quota.Used))
		}
	}
	if cfg.Paused {
		fmt.Println("Syncing is paused.")
	}
	fmt.Printf("Autostart: %v\nConfig: %s\n", autostart.Enabled(), a.cfg.Path())
	for _, pair := range cfg.Pairs {
		entries, err := a.db.Entries(pair.ID)
		if err != nil {
			return err
		}
		cursor, _ := a.db.Cursor(pair.ID)
		fmt.Printf("\n%s  %s <-> /%s (%s)\n    %d tracked items, journal cursor %d\n",
			pair.ID, pair.Local, pair.Remote, pair.Direction, len(entries), cursor)
		report, ok := a.engine.PairReport(pair.ID)
		if !ok {
			continue
		}
		if pending := report.NeedsConfirmation; pending != nil {
			fmt.Printf("    WAITING: %d deletion(s) on the server and %d on this computer need a confirmation:\n      neocloud-lite pair confirm-deletes %s\n",
				pending.RemoteDeletes, pending.LocalDeletes, pair.ID)
		}
		if report.Error != "" {
			fmt.Printf("    error: %s\n", report.Error)
		}
		for _, item := range report.NotSynced {
			fmt.Printf("    %s: %s\n", item.Path, item.Detail)
		}
	}
	return nil
}

// statusPair is one pair in `status --json`.
type statusPair struct {
	config.Pair
	Tracked int            `json:"tracked"`
	Cursor  int64          `json:"cursor"`
	Report  sync.PairState `json:"report"`
}

func printStatusJSON(a *app) error {
	cfg := a.cfg.Get()
	pairs := []statusPair{}
	for _, pair := range cfg.Pairs {
		entries, err := a.db.Entries(pair.ID)
		if err != nil {
			return err
		}
		cursor, _ := a.db.Cursor(pair.ID)
		report, _ := a.engine.PairReport(pair.ID)
		if report.NotSynced == nil {
			report.NotSynced = []sync.NotSynced{}
		}
		pairs = append(pairs, statusPair{Pair: pair, Tracked: len(entries), Cursor: cursor, Report: report})
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	return out.Encode(map[string]any{
		"loggedIn":   cfg.LoggedIn(),
		"server":     cfg.Server,
		"account":    cfg.Username,
		"deviceName": cfg.DeviceName,
		"paused":     cfg.Paused,
		"pairs":      pairs,
	})
}

func cmdAutostart(args []string) error {
	if len(args) != 1 {
		return errUsage
	}
	switch args[0] {
	case "on":
		if err := autostart.Enable(); err != nil {
			return err
		}
		fmt.Println("NeoCloud Lite now starts at login.")
	case "off":
		if err := autostart.Disable(); err != nil {
			return err
		}
		fmt.Println("Autostart removed.")
	default:
		return errUsage
	}
	return nil
}

func bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
