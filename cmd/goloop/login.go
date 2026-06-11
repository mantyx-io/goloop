package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/auth"
)

func runLogin(args []string) int {
	if len(args) > 0 && args[0] == "status" {
		return loginStatus(args[1:])
	}

	fs := flag.NewFlagSet("goloop login", flag.ExitOnError)
	browser := fs.Bool("browser", false, "Use browser callback on localhost:1455 (may conflict with Codex/Cursor)")
	apiKey := fs.Bool("api-key", false, "Store an OpenAI API key instead of ChatGPT OAuth")
	authPath := fs.String("auth-path", "", "Where to store credentials (default: ~/.goloop/auth.json)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goloop login [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Authenticate for the supervisor LLM.\n\n")
		fmt.Fprintf(os.Stderr, "  Device code (default — works on SSH, headless, alongside Codex):\n")
		fmt.Fprintf(os.Stderr, "    goloop login\n\n")
		fmt.Fprintf(os.Stderr, "  Browser callback (localhost:1455 — may conflict with Codex/Cursor):\n")
		fmt.Fprintf(os.Stderr, "    goloop login --browser\n\n")
		fmt.Fprintf(os.Stderr, "  API key (usage-based OpenAI billing):\n")
		fmt.Fprintf(os.Stderr, "    export OPENAI_API_KEY=sk-... && goloop login --api-key\n\n")
		fmt.Fprintf(os.Stderr, "Credentials are also read from ~/.codex/auth.json if you already use Codex CLI.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	path := auth.ResolveAuthPath(*authPath)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if *apiKey {
		key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		if key == "" {
			fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
			return 1
		}
		if err := auth.SaveAPIKey(path, key); err != nil {
			fmt.Fprintf(os.Stderr, "save api key: %v\n", err)
			return 1
		}
		fmt.Printf("API key saved to %s\n", path)
		return 0
	}

	var err error
	if *browser {
		err = auth.BrowserLogin(ctx, path)
	} else {
		err = auth.ChatGPTLogin(ctx, path)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
		return 1
	}
	fmt.Printf("ChatGPT login successful — credentials saved to %s\n", path)
	return 0
}

func loginStatus(args []string) int {
	fs := flag.NewFlagSet("goloop login status", flag.ExitOnError)
	authPath := fs.String("auth-path", "", "Auth file to inspect")
	_ = fs.Parse(args)

	path := auth.ResolveAuthPathForRead(*authPath)
	if !auth.IsAvailable(path) {
		fmt.Printf("Not logged in (%s missing or invalid)\n", path)
		fmt.Println("Run: goloop login")
		return 1
	}

	creds, err := auth.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
		return 1
	}

	fmt.Printf("Auth file: %s\n", path)
	fmt.Printf("Mode: %s\n", creds.Mode)
	if creds.Mode == "chatgpt" {
		if plan := auth.PlanLabel(creds); plan != "" {
			fmt.Printf("ChatGPT plan: %s\n", plan)
		}
		if !creds.ExpiresAt.IsZero() {
			fmt.Printf("Access token expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))
		}
		fmt.Println("Supervisor backend: chatgpt")
	} else {
		fmt.Println("Supervisor backend: openai (api key)")
	}
	return 0
}
