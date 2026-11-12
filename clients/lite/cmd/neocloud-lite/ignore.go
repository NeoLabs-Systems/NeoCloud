package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// cmdCheckIgnore says, like `git check-ignore -v`, whether each path is
// ignored and which rule decides: the pair's settings, the defaults or a
// .cloudignore (as it is on this computer). Like git, it exits 1 when no
// path is ignored, so scripts can test it.
func cmdCheckIgnore(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	cfg, err := config.Open(dir)
	if err != nil {
		return err
	}
	matchers := map[string]*sync.Matcher{}
	anyIgnored := false
	for _, arg := range args {
		pair, rel, err := pairOfPath(cfg.Get().Pairs, arg)
		if err != nil {
			return err
		}
		isDir := strings.HasSuffix(arg, "/") || strings.HasSuffix(arg, string(filepath.Separator))
		if info, err := os.Stat(arg); err == nil {
			isDir = info.IsDir()
		}
		m := matchers[pair.ID]
		if m == nil {
			m = sync.LocalMatcher(pair)
			matchers[pair.ID] = m
		}
		verdict := m.Explain(rel, isDir)
		anyIgnored = anyIgnored || verdict.Ignored
		fmt.Println(describeVerdict(verdict, arg))
		if err := m.Err(); err != nil {
			return err
		}
	}
	if !anyIgnored {
		os.Exit(1)
	}
	return nil
}

// describeVerdict is one output line: "<source>:<line>:<pattern>\t<path>"
// for a pattern, as git prints it.
func describeVerdict(v sync.Verdict, arg string) string {
	where := ""
	switch {
	case v.Rule != nil:
		where = fmt.Sprintf("%s:%d:%s", v.Rule.Source, v.Rule.Line, v.Rule.Pattern)
	case v.Ignored:
		where = "built-in: " + v.Builtin
	}
	state := "ignored"
	if !v.Ignored {
		state = "not ignored"
	}
	line := state + "\t" + where + "\t" + arg
	if v.Ignored && v.At != "" && !strings.HasSuffix(filepath.ToSlash(filepath.Clean(arg)), v.At) {
		line += " (in the ignored folder " + v.At + ")"
	}
	return line
}

// pairOfPath finds the pair whose local folder holds path, and the path
// relative to it.
func pairOfPath(pairs []config.Pair, path string) (config.Pair, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return config.Pair{}, "", err
	}
	abs = resolveLinks(abs)
	for _, pair := range pairs {
		rel, err := filepath.Rel(resolveLinks(pair.Local), abs)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			continue
		}
		return pair, filepath.ToSlash(rel), nil
	}
	return config.Pair{}, "", errors.New(path + " is not inside a synced folder")
}

// resolveLinks follows symbolic links in the part of path that exists
// (/tmp and /var are links on macOS), keeping the rest as it is.
func resolveLinks(path string) string {
	rest := ""
	for dir := path; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if filepath.Dir(dir) == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}
