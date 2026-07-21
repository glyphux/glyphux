package main

// §6.3 Scenario 1: local first-run opens the user's browser. This tests only
// the decision logic (should we open, and with what URL) — actually
// launching a browser is an OS side effect with nothing to assert on in CI,
// so the real opener is injected and never exercised here.

import (
	"errors"
	"io"
	"log/slog"
	"testing"
)

func TestMaybeOpenBrowserOpensWhenConfigured(t *testing.T) {
	var gotURL string
	calls := 0
	open := func(url string) error {
		calls++
		gotURL = url
		return nil
	}

	maybeOpenBrowser(true, "http://127.0.0.1:8080/", open, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if calls != 1 {
		t.Fatalf("open called %d times, want 1", calls)
	}
	if gotURL != "http://127.0.0.1:8080/" {
		t.Fatalf("open called with %q", gotURL)
	}
}

func TestMaybeOpenBrowserSkipsWhenNotConfigured(t *testing.T) {
	calls := 0
	open := func(url string) error {
		calls++
		return nil
	}

	maybeOpenBrowser(false, "http://127.0.0.1:8080/", open, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if calls != 0 {
		t.Fatalf("open called %d times, want 0", calls)
	}
}

func TestMaybeOpenBrowserLogsOpenerFailureWithoutPanicking(t *testing.T) {
	open := func(url string) error { return errors.New("no display") }
	maybeOpenBrowser(true, "http://127.0.0.1:8080/", open, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
