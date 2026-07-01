package main

import (
	"fmt"
	"os"
	"strings"
)

// version is set at link time via -ldflags in release builds.
var version = "dev"

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 2
	}

	switch args[0] {
	case "run":
		return runLoop(args[1:])
	case "configure", "config":
		return runConfigure(args[1:])
	case "init":
		return runInit(args[1:])
	case "login":
		return runLogin(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "version", "--version", "-V":
		fmt.Println(version)
		return 0
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		if strings.HasPrefix(args[0], "-") {
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n\n", args[0])
		} else {
			fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		}
		printUsage()
		return 2
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Goloop — agentic loop CLI (supervisor plans, worker executes)

Usage:
  goloop run [directory] [flags]     Run the agentic loop
  goloop configure [directory]       Global or project config
  goloop init [directory]            Initialize a project for the loop
  goloop login [flags]               Authenticate (ChatGPT or API key)
  goloop doctor [directory]          Check install, auth, and worker readiness
  goloop version                     Print version

Examples:
  goloop configure              # once: models & auth defaults
  goloop init                   # per project: goal, iters, agents
  goloop login
  goloop run .
  goloop run . --iters=30
  goloop run ./my-app -p "Focus on tests first"

Run 'goloop run --help' or 'goloop configure --help' for more flags.
`)
}
