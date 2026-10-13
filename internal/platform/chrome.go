package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Chrome struct {
	cmd       *exec.Cmd
	port      int
	browserWS string
	done      chan struct{}
	once      sync.Once
	headed    bool
}

func FindChrome(override string) (string, error) {
	for _, p := range []string{override, os.Getenv("AF_BROWSER"), "google-chrome", "chromium", "chromium-browser", "C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe"} {
		if p != "" {
			if f, err := exec.LookPath(p); err == nil {
				return f, nil
			}
		}
	}
	return "", fail("CHROME_NOT_INSTALLED_SET_BROWSER")
}
func ChromeLauncher(dataDir, executable string) Launcher {
	return func(ctx context.Context, id string, headed bool) (Browser, error) {
		return startChrome(ctx, filepath.Join(dataDir, "profiles", id), executable, headed, "https://alphafoldserver.com/")
	}
}
func startChrome(ctx context.Context, profile, exe string, headed bool, startURL string) (*Chrome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if headed && !manualDesktopAvailable() {
		return nil, fail("LOGIN_REQUIRES_SERVER_DESKTOP")
	}
	if err := os.MkdirAll(profile, 0700); err != nil {
		return nil, err
	}
	portFile := filepath.Join(profile, "DevToolsActivePort")
	if err := os.Remove(portFile); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	args := chromeArguments(profile, headed, startURL)
	c := &Chrome{cmd: exec.Command(exe, args...), done: make(chan struct{}), headed: headed}
	configureProcess(c.cmd, headed)
	c.cmd.Stdout = io.Discard
	c.cmd.Stderr = io.Discard
	if err := c.cmd.Start(); err != nil {
		return nil, fail("BROWSER_START_FAILED")
	}
	go func() { _ = c.cmd.Wait(); close(c.done) }()
	if headed {
		if err := c.ShowLogin(ctx); err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	}
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close()
			return nil, ctx.Err()
		case <-c.done:
			return nil, fail("BROWSER_EXITED_CHECK_PROFILE_OR_DISPLAY")
		case <-timer.C:
			c.Close()
			return nil, fail("BROWSER_START_TIMEOUT")
		case <-tick.C:
			b, err := os.ReadFile(portFile)
			if err != nil {
				continue
			}
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) < 2 {
				continue
			}
			port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
			if err != nil || port < 1024 || port > 65535 {
				continue
			}
			c.port = port
			c.browserWS = fmt.Sprintf("ws://127.0.0.1:%d%s", port, strings.TrimSpace(lines[1]))
			if err = validateWS(c.browserWS, port); err != nil {
				c.Close()
				return nil, err
			}
			return c, nil
		}
	}
}
func validateWS(raw string, port int) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ws" || u.Hostname() != "127.0.0.1" || u.Port() != strconv.Itoa(port) || !strings.HasPrefix(u.Path, "/devtools/") {
		return fail("INVALID_LOCAL_CDP_ENDPOINT")
	}
	return nil
}
func cdp(ctx context.Context, endpoint, method string, params any, result any) error {
	conn, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		return fail("BROWSER_DISCONNECTED")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(80 << 20)
	b, _ := json.Marshal(map[string]any{"id": 1, "method": method, "params": params})
	if err = conn.Write(ctx, websocket.MessageText, b); err != nil {
		return fail("BROWSER_DISCONNECTED")
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return fail("BROWSER_DISCONNECTED_OR_TIMEOUT")
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal(raw, &msg); err != nil {
			return fail("INVALID_CDP_RESPONSE")
		}
		if msg.ID != 1 {
			continue
		}
		if len(msg.Error) > 0 {
			return fail("BROWSER_COMMAND_FAILED")
		}
		if result != nil {
			return json.Unmarshal(msg.Result, result)
		}
		return nil
	}
}
func (c *Chrome) Eval(ctx context.Context, expression string, out any) error {
	if c.headed {
		return fail("MANUAL_LOGIN_INTERACTIVE_ONLY")
	}
	select {
	case <-c.done:
		return fail("BROWSER_DISCONNECTED")
	default:
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/json/list", c.port), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	r, err := client.Do(req)
	if err != nil {
		return fail("BROWSER_DISCONNECTED")
	}
	defer r.Body.Close()
	var pages []struct{ Type, URL, WebSocketDebuggerURL string }
	if err = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&pages); err != nil {
		return fail("INVALID_CDP_RESPONSE")
	}
	endpoint := ""
	for _, p := range pages {
		u, _ := url.Parse(p.URL)
		if p.Type == "page" && u != nil && u.Scheme == "https" && u.Host == "alphafoldserver.com" {
			endpoint = p.WebSocketDebuggerURL
			break
		}
	}
	// about:blank is allowed only in the explicit Chrome smoke test, not an upstream request.
	if endpoint == "" && expression == "1+1" {
		for _, p := range pages {
			if p.Type == "page" && p.URL == "about:blank" {
				endpoint = p.WebSocketDebuggerURL
				break
			}
		}
	}
	if endpoint == "" {
		return fail("PLATFORM_PAGE_NOT_READY")
	}
	if err = validateWS(endpoint, c.port); err != nil {
		return err
	}
	var reply struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err = cdp(ctx, endpoint, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}, &reply); err != nil {
		return err
	}
	if len(reply.Exception) > 0 {
		return fail("BROWSER_EVALUATION_FAILED")
	}
	if len(reply.Result.Value) == 0 {
		return fail("EMPTY_BROWSER_RESULT")
	}
	return json.Unmarshal(reply.Result.Value, out)
}
func (c *Chrome) Close() error {
	c.once.Do(func() {
		if c.headed && c.cmd.Process != nil && c.Alive() {
			closeManualBrowser(c.cmd.Process)
		}
		if c.browserWS != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = cdp(ctx, c.browserWS, "Browser.close", map[string]any{}, nil)
			cancel()
		}
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			<-c.done
		}
	})
	return nil
}

func (c *Chrome) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Present the ordinary authentication window through the OS, never through CDP.
func (c *Chrome) ShowLogin(ctx context.Context) error {
	if !c.headed {
		return fail("LOGIN_REQUIRES_VISIBLE_BROWSER")
	}
	return c.showManualLogin(ctx)
}
