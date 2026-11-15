package handoff

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func fakeRun(out string, code int, seen *[]string) runner {
	return func(_ context.Context, env []string, name string, args ...string) (string, int) {
		*seen = append(append(*seen, name), env...)
		*seen = append(*seen, args...)
		return out, code
	}
}

func found(string) (string, error)   { return "/usr/bin/x", nil }
func missing(string) (string, error) { return "", errors.New("not found") }

func TestMacAnswers(t *testing.T) {
	for out, want := range map[string]Choice{"Decline\n": ChoiceDecline, "Choose folder…\n": ChoiceFolder, "Save to Downloads\n": ChoiceDownloads} {
		var seen []string
		got, _ := nativePrompt{goos: "darwin", lookPath: found, run: fakeRun(out, 0, &seen)}.Ask(context.Background(), "msg")
		if got != want {
			t.Errorf("%q: got %v", out, got)
		}
		if seen[len(seen)-1] != "msg" {
			t.Error("the message must be an argument, not part of the script")
		}
	}
	var seen []string
	if got, _ := (nativePrompt{goos: "darwin", run: fakeRun("", 1, &seen)}).Ask(context.Background(), "m"); got != ChoiceLater {
		t.Errorf("Esc: %v", got)
	}
}

func TestWindowsPassesMessageThroughEnvironment(t *testing.T) {
	var seen []string
	got, _ := nativePrompt{goos: "windows", run: fakeRun("No\r\n", 0, &seen)}.Ask(context.Background(), `x"; calc; "`)
	if got != ChoiceFolder {
		t.Errorf("got %v", got)
	}
	if strings.Contains(strings.Join(seen[2:], " "), "calc") {
		t.Error("the message reached the script text")
	}
	if got, _ := (nativePrompt{goos: "windows", run: fakeRun("Cancel", 0, &seen)}).Ask(context.Background(), "m"); got != ChoiceDecline {
		t.Errorf("cancel: %v", got)
	}
}

func TestLinuxZenityKdialogAndNone(t *testing.T) {
	var seen []string
	cases := []struct {
		out  string
		code int
		want Choice
	}{{"", 0, ChoiceDownloads}, {"Choose folder…\n", 1, ChoiceFolder}, {"", 1, ChoiceDecline}, {"", 5, ChoiceLater}}
	for _, c := range cases {
		got, _ := nativePrompt{goos: "linux", lookPath: found, run: fakeRun(c.out, c.code, &seen)}.Ask(context.Background(), "m")
		if got != c.want {
			t.Errorf("zenity %+v: got %v", c, got)
		}
	}
	only := func(name string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == name {
				return "/x", nil
			}
			return "", errors.New("no")
		}
	}
	for code, want := range map[int]Choice{0: ChoiceDownloads, 1: ChoiceFolder, 2: ChoiceDecline} {
		got, _ := nativePrompt{goos: "linux", lookPath: only("kdialog"), run: fakeRun("", code, &seen)}.Ask(context.Background(), "m")
		if got != want {
			t.Errorf("kdialog %d: got %v", code, got)
		}
	}
	if got, _ := (nativePrompt{goos: "linux", lookPath: missing}).Ask(context.Background(), "m"); got != ChoiceUnavailable {
		t.Errorf("no dialog: %v", got)
	}
}
