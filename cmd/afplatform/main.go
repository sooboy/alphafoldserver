package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/sooboy/alphafoldserver/internal/platform"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	err := runCommand(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func runCommand(args []string) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Print(`AlphaFold Team Platform

Usage:
  afplatform <command> [flags]

Commands:
  serve               Start the team platform HTTP service
  init                Legacy optional CLI administrator initialization (not required)
  import-node-config  Import legacy account IDs/labels only (not Google sessions)
  help [command]      Show global or command-specific help

Quick start (web setup on first run, no init required):
  afplatform

Open: http://127.0.0.1:8788
Default data: ./private/go-platform
First run: open the website and create the administrator
Setup token file: <data>/setup-token.txt (enter once on the setup page)
Runtime requirement: installed Chrome/Chromium or Edge; no Node.js required.

Help: afplatform -h | afplatform --help | afplatform serve -h
`)
		return nil
	}
	if args[0] == "help" {
		if len(args) != 2 || (args[1] != "init" && args[1] != "serve" && args[1] != "import-node-config") {
			return fmt.Errorf("usage: afplatform help [init|serve|import-node-config]")
		}
		return runCommand([]string{args[1], "-h"})
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	data := f.String("data", "./private/go-platform", "persistent private data directory")
	switch args[0] {
	case "import-node-config":
		input := f.String("input", "", "legacy Node state.json; imports account IDs/labels ONLY")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *input == "" {
			return fmt.Errorf("INPUT_REQUIRED")
		}
		b, err := os.ReadFile(*input)
		if err != nil {
			return err
		}
		var legacy struct {
			Accounts []struct {
				ID      string `json:"id"`
				Label   string `json:"label"`
				Enabled bool   `json:"enabled"`
			} `json:"accounts"`
		}
		if err = json.Unmarshal(b, &legacy); err != nil {
			return fmt.Errorf("INVALID_LEGACY_STATE")
		}
		s, err := platform.OpenStore(*data)
		if err != nil {
			return err
		}
		defer s.Close()
		if len(s.Snapshot().Users) == 0 {
			return fmt.Errorf("WEB_SETUP_REQUIRED: start afplatform and create the administrator on the website before importing accounts; init is optional")
		}
		count := 0
		err = s.Update(func(st *platform.State) error {
			valid := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
			for _, a := range legacy.Accounts {
				if !valid.MatchString(a.ID) || len(a.Label) > 160 {
					return fmt.Errorf("INVALID_ACCOUNT")
				}
				if _, ok := st.Accounts[a.ID]; ok {
					continue
				}
				if len(st.Accounts) >= 1000 {
					return fmt.Errorf("ACCOUNT_LIMIT")
				}
				st.Accounts[a.ID] = platform.Account{ID: a.ID, Label: a.Label, Enabled: a.Enabled, Status: "needs_login"}
				count++
			}
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Printf("Imported %d account configurations only; browser sessions, credentials and historical jobs were NOT copied. Administrator login required.\n", count)
		return nil
	case "init":
		name := f.String("admin", "admin", "initial admin username")
		passFile := f.String("password-file", "", "read password from a private file (otherwise generates a private file)")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		s, err := platform.OpenStore(*data)
		if err != nil {
			return err
		}
		defer s.Close()
		if len(s.Snapshot().Users) > 0 {
			return fmt.Errorf("ALREADY_INITIALIZED")
		}
		var password string
		generated := false
		out := filepath.Join(s.Dir, "initial-admin-password.txt")
		if *passFile != "" {
			b, err := os.ReadFile(*passFile)
			if err != nil {
				return err
			}
			password = strings.TrimSpace(string(b))
		} else {
			password = platform.ID() + platform.ID()
			file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, err = file.WriteString(password + "\n")
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			generated = true
		}
		_, err = platform.CreateUser(s, *name, password, "admin", "local-setup")
		if err != nil {
			return err
		}
		fmt.Printf("Initialized %s; admin=%s\n", s.Dir, *name)
		if generated {
			fmt.Printf("Password stored privately at %s (not printed); remove after secure storage.\n", out)
		}
		return nil
	case "serve":
		listen := f.String("listen", "127.0.0.1:8788", "listen address")
		public := f.String("public-url", "http://127.0.0.1:8788", "exact browser origin, no trailing slash")
		browser := f.String("browser", "", "Chrome/Chromium executable, or AF_BROWSER")
		limit := f.Int("max-active-browsers", 3, "maximum simultaneously active browser instances including login windows")
		idle := f.Duration("browser-idle", 2*time.Minute, "close idle headless browser after this interval")
		cert := f.String("tls-cert", "", "TLS certificate file")
		key := f.String("tls-key", "", "TLS key file")
		allowHTTP := f.Bool("allow-insecure-http", false, "allow unencrypted HTTP on a trusted private LAN (private IP public URL; never expose via a public proxy)")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *limit < 1 || *limit > 20 || *idle < time.Second {
			return fmt.Errorf("INVALID_BROWSER_LIMIT_OR_IDLE")
		}
		if err := validateServerNetwork(*listen, *public, *cert, *key, *allowHTTP); err != nil {
			return err
		}
		s, err := platform.OpenStore(*data)
		if err != nil {
			return err
		}
		defer s.Close()
		p := platform.NewBrowserPool(*limit, *idle, func(ctx context.Context, id string, headed bool) (platform.Browser, error) {
			exe, err := platform.FindChrome(*browser)
			if err != nil {
				return nil, err
			}
			return platform.ChromeLauncher(s.Dir, exe)(ctx, id, headed)
		})
		engine := platform.NewEngine(s, p)
		handler, err := platform.NewServer(engine, *public)
		if err != nil {
			return err
		}
		handler.PrivateNetworkOnly = *allowHTTP
		if *allowHTTP {
			fmt.Fprintln(os.Stderr, "WARNING: LAN HTTP enabled. Passwords, sessions and research data are unencrypted. Use a trusted private network and restrict the firewall; do not expose through a public proxy or port forwarding.")
		}
		if len(s.Snapshot().Users) == 0 {
			fmt.Printf("First run: create your administrator on the website. Setup token file: %s\n", filepath.Join(s.Dir, "setup-token.txt"))
		}
		server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		engine.Start(*limit)
		defer engine.Stop()
		defer listener.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		done := make(chan error, 1)
		go func() {
			if *cert != "" {
				done <- server.ServeTLS(listener, *cert, *key)
			} else {
				done <- server.Serve(listener)
			}
		}()
		fmt.Printf("AlphaFold team platform: %s; browser limit=%d; data=%s\n", *public, *limit, s.Dir)
		select {
		case err := <-done:
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		case <-ctx.Done():
			engine.Stop()
			c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return server.Shutdown(c)
		}
	default:
		return fmt.Errorf("unknown command %s", args[0])
	}
}

func validateServerNetwork(listen, public, cert, key string, allowHTTP bool) error {
	u, err := url.Parse(public)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	loopback := ip != nil && ip.IsLoopback()
	if allowHTTP {
		if u.Scheme != "http" || cert != "" || key != "" {
			return fmt.Errorf("INSECURE_HTTP_REQUIRES_HTTP_WITHOUT_TLS")
		}
		publicIP := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && !(publicIP.IsLoopback() || publicIP.IsPrivate()) {
			return fmt.Errorf("LAN_HTTP_REQUIRES_PRIVATE_IP_PUBLIC_URL")
		}
		if ip == nil || !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()) {
			return fmt.Errorf("LAN_HTTP_REQUIRES_PRIVATE_OR_WILDCARD_LISTENER")
		}
		return nil
	}
	if u.Scheme != "https" && (!loopback || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return fmt.Errorf("HTTPS_REQUIRED_FOR_TEAM_ACCESS")
	}
	if (cert == "") != (key == "") {
		return fmt.Errorf("TLS_CERT_AND_KEY_REQUIRED")
	}
	if !loopback && cert == "" {
		return fmt.Errorf("NON_LOOPBACK_LISTENER_REQUIRES_TLS; or use a loopback HTTPS reverse proxy")
	}
	return nil
}
