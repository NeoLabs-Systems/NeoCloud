package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/handoff"
	"github.com/neolabs/neocloud/clients/lite/internal/notify"
	"github.com/neolabs/neocloud/clients/lite/internal/settingsui"
)

// cmdSend sends files to one of the account's other online devices:
//
//	neocloud-lite send [--to NAME|ID] [--no-wait] FILE...
//
// It makes the call itself with the stored token, so it works whether or not
// the tray app is running. Without a terminal (the Windows Send to menu, a
// Linux file manager) it asks with native dialogs and reports with
// notifications instead.
func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	to := fs.String("to", "", "the device's name or id (see the list printed when it is missing)")
	noWait := fs.Bool("no-wait", false, "return once the files are uploaded instead of waiting for the other device")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	tty := term.IsTerminal(int(os.Stderr.Fd()))
	if len(paths) == 0 && tty {
		return errUsage
	}
	a, err := openApp(stderrLogger(true))
	if err != nil {
		return err
	}
	defer a.close()
	ctx, cancel := signalContext()
	defer cancel()
	if err := keyLock(a).Unlock(ctx); err != nil {
		return err
	}
	sender := handoff.NewSender(ctx, a.cfg, stderrLogger(true))
	if *to == "" && !tty {
		return sendWithDialogs(ctx, sender, paths)
	}
	items, err := handoff.Prepare(paths)
	if err != nil {
		return err
	}
	devices, err := sender.Devices(ctx)
	if err != nil {
		return err
	}
	device, err := chooseDevice(devices, *to, os.Stdin, os.Stderr, tty && term.IsTerminal(int(os.Stdin.Fd())))
	if err != nil {
		return err
	}
	progress := io.Discard
	if tty {
		progress = os.Stderr
	}
	final := sender.Send(ctx, device, items, handoff.Options{NoWait: *noWait, Update: progressPrinter(progress)})
	return report(final, os.Stdout, *noWait)
}

// sendWithDialogs is `send` without a terminal: native dialogs ask, and the
// result comes as notifications.
func sendWithDialogs(ctx context.Context, sender *handoff.Sender, paths []string) error {
	sender.Notify = func(title, body string) { _ = notify.Show(title, body) }
	flow := &handoff.Flow{Sender: sender, Dialogs: handoff.NewNativeDialogs(), ChooseFiles: settingsui.ChooseFiles, Foreground: true}
	final, started := flow.Run(ctx, paths)
	if started && final.State == handoff.StateFailed {
		return errors.New(final.Message)
	}
	return nil
}

// chooseDevice picks the device to send to: the one asked for, the only one
// online, or (several, none asked for) the one the person types when a
// person is there to ask.
func chooseDevice(devices []api.HandoffDevice, query string, in io.Reader, out io.Writer, interactive bool) (api.HandoffDevice, error) {
	if len(devices) == 0 {
		return api.HandoffDevice{}, errors.New(handoff.NoDeviceMessage)
	}
	if query != "" {
		return handoff.ResolveDevice(devices, query)
	}
	if len(devices) == 1 {
		return devices[0], nil
	}
	labels := handoff.DeviceLabels(devices)
	fmt.Fprintln(out, "Several devices are online:")
	for i, label := range labels {
		fmt.Fprintf(out, "  %d) %s\n", i+1, label)
	}
	if !interactive {
		return api.HandoffDevice{}, errors.New("choose one with --to <name or id>")
	}
	fmt.Fprintf(out, "Send to which one? [1-%d]: ", len(devices))
	line, err := bufio.NewReader(in).ReadString('\n')
	choice, convErr := strconv.Atoi(strings.TrimSpace(line))
	if (err != nil && line == "") || convErr != nil || choice < 1 || choice > len(devices) {
		return api.HandoffDevice{}, errors.New("no device chosen")
	}
	return devices[choice-1], nil
}

// progressPrinter rewrites one line with how far the upload is.
func progressPrinter(out io.Writer) func(handoff.Transfer) {
	var last time.Time
	return func(t handoff.Transfer) {
		if out == io.Discard || (t.Active && t.State == handoff.StateUploading && time.Since(last) < 200*time.Millisecond) {
			return
		}
		last = time.Now()
		switch t.State {
		case handoff.StateUploading:
			fmt.Fprintf(out, "\rSending %s to %s: %s of %s   ", t.Label, t.To, bytes(t.Done), bytes(t.Size))
		case handoff.StateWaiting:
			fmt.Fprintf(out, "\rUploaded %s. Waiting for %s to accept…        ", t.Label, t.To)
		case handoff.StateReceiving:
			fmt.Fprintf(out, "\r%s is receiving %s…                    ", t.To, t.Label)
		}
		if !t.Active {
			fmt.Fprintln(out)
		}
	}
}

// report turns how a send ended into the command's result.
func report(t handoff.Transfer, out io.Writer, noWait bool) error {
	switch t.State {
	case handoff.StateDelivered:
		fmt.Fprintf(out, "Delivered %s to %s.\n", t.Label, t.To)
	case handoff.StateWaiting:
		if noWait {
			fmt.Fprintf(out, "Uploaded %s. %s gets it when they accept.\n", t.Label, t.To)
			return nil
		}
		return errors.New("the send did not finish")
	case handoff.StateDeclined:
		return fmt.Errorf("%s declined %s", t.To, t.Label)
	case handoff.StateExpired:
		return fmt.Errorf("%s did not receive %s in time", t.To, t.Label)
	case handoff.StateCancelled:
		return errors.New("cancelled")
	default:
		return errors.New(t.Message)
	}
	return nil
}
