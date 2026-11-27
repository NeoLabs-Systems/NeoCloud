// Command neocloud-release is the release tooling CI runs: resolving and
// stamping versions, checking the web client bundle, building the signed
// runtime archives installs update themselves with and rendering the download
// section of a release's notes.
//
//	go run ./cmd/neocloud-release <command> [flags]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "neocloud-release:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: neocloud-release <version|resolve-version|set-version|check-web|runtime-bundles|runtime-keygen|release-download> [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	channel := flags.String("channel", "beta", "")
	githubOutput := flags.Bool("github-output", false, "")
	tag := flags.String("tag", "", "")
	repo := flags.String("repo", "", "")
	dryRun := flags.Bool("dry-run", false, "")
	output := flags.String("output", "dist", "")
	version := flags.String("version", "", "")
	web := flags.String("web", webDir, "")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "version":
		current, err := readVersion(".")
		if err != nil {
			return err
		}
		fmt.Println(current)
	case "resolve-version":
		tags, err := exec.CommandContext(ctx, "git", "tag", "--list", "v[0-9]*.[0-9]*.[0-9]*").Output()
		if err != nil {
			return fmt.Errorf("list git tags: %w", err)
		}
		current, err := readVersion(".")
		if err != nil {
			return err
		}
		next, err := nextVersion(current, strings.Fields(string(tags)), *channel)
		if err != nil {
			return err
		}
		if path := os.Getenv("GITHUB_OUTPUT"); *githubOutput && path != "" {
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(file, "version=%s\nrelease_tag=v%s\n", next, next)
			if err = errors.Join(err, file.Close()); err != nil {
				return err
			}
		}
		fmt.Println(next)
	case "set-version":
		if flags.NArg() != 1 {
			return errors.New("usage: neocloud-release set-version VERSION")
		}
		return setVersion(".", flags.Arg(0))
	case "check-web":
		if err := checkWebBundle(".", *web); err != nil {
			return err
		}
		fmt.Println("Flutter web bundle is local and version-consistent:", *web)
	case "runtime-bundles":
		return buildRuntimeBundles(".", *output, *version, os.Getenv(signingKeyEnv))
	case "runtime-keygen":
		return writeKeypair(*output)
	case "release-download":
		if *tag == "" {
			return errors.New("--tag is required")
		}
		return updateDownloadNotes(ctx, *tag, *repo, *dryRun)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}
