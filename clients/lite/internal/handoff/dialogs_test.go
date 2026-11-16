package handoff

import (
	"context"
	"strings"
	"testing"
)

func dialogs(goos string, lookPath func(string) (string, error), run runner) nativeDialogs {
	return nativeDialogs{goos: goos, lookPath: lookPath, run: run}
}

func TestChooseMacPassesTextAsArguments(t *testing.T) {
	var seen []string
	d := dialogs("darwin", found, fakeRun("Firefox (browser)\n", 0, &seen))
	index, err := d.Choose(context.Background(), `Send "x"; do shell script "id"`, []string{"Pixel (phone)", "Firefox (browser)"})
	if err != nil || index != 1 {
		t.Fatalf("%d %v", index, err)
	}
	joined := strings.Join(seen, "|")
	if strings.Contains(seen[2], "do shell script") || !strings.Contains(joined, `do shell script "id"`) {
		t.Errorf("the message belongs in argv, not in the script: %q", seen)
	}
	if _, err := dialogs("darwin", found, fakeRun("", 1, &seen)).Choose(context.Background(), "m", []string{"a"}); err != ErrCancelled {
		t.Errorf("cancel: %v", err)
	}
}

func TestChooseWindowsUsesTheEnvironmentAndAnIndex(t *testing.T) {
	var seen []string
	d := dialogs("windows", found, fakeRun("1\r\n", 0, &seen))
	index, err := d.Choose(context.Background(), "msg", []string{"A", "B"})
	if err != nil || index != 1 {
		t.Fatalf("%d %v", index, err)
	}
	if !strings.Contains(strings.Join(seen, "|"), "NEOCLOUD_ITEMS=A\nB") {
		t.Errorf("items: %q", seen)
	}
	for _, out := range []string{"", "9", "x"} {
		if _, err := dialogs("windows", found, fakeRun(out, 0, &seen)).Choose(context.Background(), "m", []string{"A", "B"}); err != ErrCancelled {
			t.Errorf("%q: %v", out, err)
		}
	}
}

func TestChooseLinux(t *testing.T) {
	var seen []string
	index, err := dialogs("linux", found, fakeRun("B\n", 0, &seen)).Choose(context.Background(), "m", []string{"A", "B"})
	if err != nil || index != 1 || seen[0] != "zenity" {
		t.Errorf("zenity: %d %v %q", index, err, seen)
	}
	onlyKDE := func(name string) (string, error) {
		if name == "kdialog" {
			return "/usr/bin/kdialog", nil
		}
		return missing(name)
	}
	seen = nil
	index, err = dialogs("linux", onlyKDE, fakeRun("1\n", 0, &seen)).Choose(context.Background(), "m", []string{"A", "B"})
	if err != nil || index != 1 || seen[0] != "kdialog" {
		t.Errorf("kdialog: %d %v %q", index, err, seen)
	}
	if _, err := dialogs("linux", missing, fakeRun("", 0, &seen)).Choose(context.Background(), "m", []string{"A"}); err != ErrNoDialog {
		t.Errorf("none: %v", err)
	}
}

func TestConfirmAndInform(t *testing.T) {
	var seen []string
	if ok, _ := dialogs("darwin", found, fakeRun("", 0, &seen)).Confirm(context.Background(), "m", "Send"); !ok {
		t.Error("mac ok")
	}
	if ok, _ := dialogs("darwin", found, fakeRun("", 1, &seen)).Confirm(context.Background(), "m", "Send"); ok {
		t.Error("mac cancel")
	}
	if ok, _ := dialogs("windows", found, fakeRun("OK\r\n", 0, &seen)).Confirm(context.Background(), "m", "Send"); !ok {
		t.Error("windows ok")
	}
	if ok, _ := dialogs("windows", found, fakeRun("Cancel\r\n", 0, &seen)).Confirm(context.Background(), "m", "Send"); ok {
		t.Error("windows cancel")
	}
	if ok, err := dialogs("linux", missing, fakeRun("", 0, &seen)).Confirm(context.Background(), "m", "Send"); ok || err != ErrNoDialog {
		t.Errorf("linux none: %v %v", ok, err)
	}
	if err := dialogs("linux", missing, fakeRun("", 0, &seen)).Inform(context.Background(), "m"); err != ErrNoDialog {
		t.Errorf("inform none: %v", err)
	}
	seen = nil
	if err := dialogs("linux", found, fakeRun("", 0, &seen)).Inform(context.Background(), "hello"); err != nil || seen[0] != "zenity" {
		t.Errorf("inform: %v %q", err, seen)
	}
}
