package main

import (
	"strings"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/handoff"
)

var twoDevices = []api.HandoffDevice{{ID: "d:1", Name: "Pixel 8", Kind: "phone"}, {ID: "s:2", Name: "Firefox", Kind: "browser"}}

func TestChooseDeviceByNameOrTheOnlyOne(t *testing.T) {
	var out strings.Builder
	if d, err := chooseDevice(twoDevices, "firefox", nil, &out, false); err != nil || d.ID != "s:2" {
		t.Errorf("--to: %v %v", d, err)
	}
	if d, err := chooseDevice(twoDevices[:1], "", nil, &out, false); err != nil || d.ID != "d:1" {
		t.Errorf("single: %v %v", d, err)
	}
	if _, err := chooseDevice(nil, "", nil, &out, false); err == nil || !strings.Contains(err.Error(), "No other device is online right now") {
		t.Errorf("none: %v", err)
	}
	if _, err := chooseDevice(twoDevices, "Tablet", nil, &out, false); err == nil {
		t.Error("unknown device")
	}
}

func TestChooseDeviceListsAndExitsWithoutATerminal(t *testing.T) {
	var out strings.Builder
	_, err := chooseDevice(twoDevices, "", strings.NewReader("1\n"), &out, false)
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("error: %v", err)
	}
	if !strings.Contains(out.String(), "1) Pixel 8 (phone)") || !strings.Contains(out.String(), "2) Firefox (browser)") {
		t.Errorf("list: %q", out.String())
	}
}

func TestChooseDevicePromptsOnATerminal(t *testing.T) {
	var out strings.Builder
	d, err := chooseDevice(twoDevices, "", strings.NewReader("2\n"), &out, true)
	if err != nil || d.ID != "s:2" {
		t.Errorf("%v %v", d, err)
	}
	for _, bad := range []string{"", "0\n", "3\n", "x\n"} {
		if _, err := chooseDevice(twoDevices, "", strings.NewReader(bad), &out, true); err == nil {
			t.Errorf("%q chose a device", bad)
		}
	}
}

func TestReportMapsOutcomesToResults(t *testing.T) {
	var out strings.Builder
	if err := report(handoff.Transfer{State: handoff.StateDelivered, Label: "a.txt", To: "Pixel 8"}, &out, false); err != nil || !strings.Contains(out.String(), "Delivered a.txt to Pixel 8.") {
		t.Errorf("delivered: %v %q", err, out.String())
	}
	if err := report(handoff.Transfer{State: handoff.StateWaiting, Label: "a.txt", To: "Pixel 8"}, &out, true); err != nil {
		t.Errorf("no-wait: %v", err)
	}
	for state, want := range map[string]string{
		handoff.StateDeclined: "Pixel 8 declined a.txt", handoff.StateExpired: "did not receive a.txt in time",
		handoff.StateCancelled: "cancelled", handoff.StateFailed: "boom",
	} {
		err := report(handoff.Transfer{State: state, Label: "a.txt", To: "Pixel 8", Message: "boom"}, &out, false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", state, err)
		}
	}
}
