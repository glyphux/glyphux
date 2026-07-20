package main

import (
	"log/slog"
	"os/exec"
	"runtime"
)

// openBrowserFunc launches the given URL in the user's default browser.
// Injectable so tests never actually launch one.
type openBrowserFunc func(url string) error

// maybeOpenBrowser opens url in the local browser when shouldOpen is set
// (§6.3 Scenario 1: local self-hosted first run). Server/cloud scenarios
// leave shouldOpen false — there is no desktop window to open. A failure to
// launch a browser is not fatal to the daemon; it's logged and ignored.
func maybeOpenBrowser(shouldOpen bool, url string, open openBrowserFunc, log *slog.Logger) {
	if !shouldOpen {
		return
	}
	if err := open(url); err != nil {
		log.Warn("failed to open browser", "url", url, "error", err)
	}
}

// openBrowser is the real, OS-dependent opener.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
