package handoff

import (
	"context"
	"errors"
	"fmt"
	gosync "sync"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// Flow is the send as the tray menu and the file manager shortcuts run it:
// choose files (unless given), choose the device with native dialogs, then
// send in the background. It speaks to the person only through dialogs, and
// through Notify where this system has none.
type Flow struct {
	Sender  *Sender
	Dialogs Dialogs
	// ChooseFiles shows the file dialog (ok false: cancelled or none here).
	ChooseFiles func(ctx context.Context) (paths []string, ok bool)

	// Foreground makes Run send in place and return when the send ended
	// (the command line must not exit before then) instead of in the background.
	Foreground bool

	busy gosync.Mutex // one question at a time
}

// NoDeviceMessage is what the person is told when nothing can be sent to.
const NoDeviceMessage = "No other device is online right now. Open NeoCloud on your phone or in a browser, then try again."

// Run asks what is missing and starts the send; it reports whether one was
// started (false: nothing to send, or the person cancelled). paths may be empty.
func (f *Flow) Run(ctx context.Context, paths []string) (sent Transfer, started bool) {
	if !f.busy.TryLock() {
		return Transfer{}, false
	}
	defer f.busy.Unlock()
	if len(paths) == 0 {
		if f.ChooseFiles == nil {
			return Transfer{}, false
		}
		var ok bool
		if paths, ok = f.ChooseFiles(ctx); !ok {
			return Transfer{}, false
		}
	}
	items, err := Prepare(paths)
	if err != nil {
		f.tell(ctx, err.Error())
		return Transfer{}, false
	}
	devices, err := f.Sender.Devices(ctx)
	if err != nil {
		f.tell(ctx, err.Error())
		return Transfer{}, false
	}
	if len(devices) == 0 {
		f.tell(ctx, NoDeviceMessage)
		return Transfer{}, false
	}
	device, ok := f.pick(ctx, items, devices)
	if !ok {
		return Transfer{}, false
	}
	if f.Foreground {
		return f.Sender.Send(ctx, device, items, Options{}), true
	}
	f.Sender.Start(device, items)
	return Transfer{}, true
}

// pick chooses the device: a list for several, a one-click question for one.
func (f *Flow) pick(ctx context.Context, items []Item, devices []api.HandoffDevice) (api.HandoffDevice, bool) {
	what := fmt.Sprintf("%s (%s)", Describe(items), humanSize(TotalSize(items)))
	if len(devices) == 1 {
		ok, err := f.Dialogs.Confirm(ctx, fmt.Sprintf("Send %s to %s?", what, safeLabel(devices[0].Name)), "Send")
		if errors.Is(err, ErrNoDialog) {
			return devices[0], true // nothing to ask with; the person started this
		}
		return devices[0], ok && err == nil
	}
	index, err := f.Dialogs.Choose(ctx, "Send "+what+" to:", DeviceLabels(devices))
	if errors.Is(err, ErrNoDialog) {
		f.Sender.notify("Choose a device", "Several devices are online. Run: neocloud-lite send --to <device> <files>")
	}
	return devices[min(max(index, 0), len(devices)-1)], err == nil
}

// tell shows a message in a dialog, or as a notification without one.
func (f *Flow) tell(ctx context.Context, message string) {
	if err := f.Dialogs.Inform(ctx, message); err != nil {
		f.Sender.notify("NeoCloud", message)
	}
}
