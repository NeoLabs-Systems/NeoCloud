package manager

import (
	"slices"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

// argSpec is everything a command accepts. A command that changes something
// refuses anything else, so a typo never runs it.
type argSpec struct {
	flags, valued []string
	max           int // positional arguments
	usage         string
}

var argSpecs = map[string]argSpec{
	"install":     {valued: []string{"--channel", "--port", "--public-url"}, usage: "install [--channel stable|beta] [--port N] [--public-url URL]"},
	"setup":       {valued: []string{"--channel", "--port", "--public-url"}, usage: "setup [--channel stable|beta] [--port N] [--public-url URL]"},
	"start":       {flags: []string{"--foreground"}, usage: "start [--foreground]"},
	"stop":        {usage: "stop"},
	"restart":     {usage: "restart"},
	"status":      {usage: "status"},
	"doctor":      {flags: []string{"--full"}, usage: "doctor [--full]"},
	"logs":        {flags: []string{"-f", "--follow"}, usage: "logs [-f]"},
	"admin":       {max: 2, usage: "admin [list | grant <username> | revoke <username>]"},
	"backup":      {flags: []string{"--bulk"}, valued: []string{"--from"}, max: 3, usage: backupUsage},
	"keys":        {max: 1, usage: "keys export"},
	"update":      {flags: []string{"--check", "--notes"}, valued: []string{"--tag"}, max: 1, usage: "update [stable|beta] [--tag <version>] [--check]"},
	"rollback":    {usage: "rollback"},
	"channel":     {max: 1, usage: "channel [stable|beta]"},
	"repair":      {usage: "repair"},
	"uninstall":   {usage: "uninstall"},
	"serve":       {usage: "serve"},
	"healthcheck": {usage: "healthcheck"},
}

// withoutDetails drops --details, which every command accepts: it only adds
// technical details to a failure.
func withoutDetails(args []string) []string {
	if len(args) > 0 && args[0] == "env" {
		return args
	}
	return slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == "--details" })
}

// helpRequested reports a help flag, which shows help and changes nothing.
// `env` takes free text, so only its first argument counts there.
func helpRequested(command string, args []string) bool {
	if command == "env" && len(args) > 0 {
		args = args[:1]
	}
	return slices.Contains(args, "--help") || slices.Contains(args, "-h")
}

func checkArgs(command string, args []string) error {
	spec, known := argSpecs[command]
	if !known {
		return nil
	}
	refuse := func(problem string) error { return inputError(problem + "Usage: neocloud " + spec.usage) }
	positional := 0
	for i := 0; i < len(args); i++ {
		name, value, joined := strings.Cut(args[i], "=")
		switch {
		case slices.Contains(spec.flags, args[i]):
		case slices.Contains(spec.valued, name):
			if !joined && i+1 < len(args) {
				i++
				value = args[i]
			}
			// An empty --public-url clears the setting.
			if strings.HasPrefix(value, "-") || value == "" && name != "--public-url" {
				return refuse(name + " needs a value. ")
			}
		case strings.HasPrefix(args[i], "-"):
			return refuse("Unknown option " + args[i] + ". ")
		default:
			positional++
		}
	}
	if positional > spec.max {
		return refuse("")
	}
	return nil
}

// channelArg is the channel a command line names, as `--channel x` or as a bare
// word; "" when it names none.
func channelArg(command string, args []string) (string, error) {
	value, named := cli.FlagValue(args, "--channel")
	if !named {
		if positional := cli.Positionals(args, "--channel", "--tag", "--port", "--public-url"); len(positional) > 0 {
			value, named = positional[0], true
		}
	}
	if !named {
		return "", nil
	}
	channel := release.ParseChannel(value)
	if channel == "" {
		return "", inputError("Usage: neocloud " + argSpecs[command].usage)
	}
	return channel, nil
}
