package platform

import (
	"context"
	"time"
)

func chromeArguments(profile string, headed bool, startURL string) []string {
	args := []string{"--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check"}
	if headed {
		// Ordinary, human-operated authentication. No CDP, WebDriver, injected
		// scripts, credential capture, or altered browser identification.
		return append(args, "--new-window", startURL)
	}
	return append(args, "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1", "--disable-background-networking", "--disable-background-timer-throttling", "--disable-renderer-backgrounding", "--headless=new", startURL)
}

func (c *Chrome) showManualLogin(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-c.done:
			return fail("BROWSER_DISCONNECTED")
		default:
		}
		shown, err := presentManualBrowser(c.cmd.Process.Pid)
		if err != nil {
			return err
		}
		if shown {
			return nil
		}
		select {
		case <-ctx.Done():
			return fail("LOGIN_WINDOW_UNAVAILABLE")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
