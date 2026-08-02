// Ticket T10b RED: `glyphux instance open` — the browser opener contract.
package instance

import (
	"context"
	"errors"
	"testing"
)

// recordingOpener records every URL and returns nil.
type recordingOpener struct{ urls []string }

func (r *recordingOpener) Open(u string) error {
	r.urls = append(r.urls, u)
	return nil
}

type errOpener struct{}

func (errOpener) Open(string) error { return errors.New("browser failed") }

// --- Acceptance criterion: Open derives the setup URL from the config
// listen address and invokes the opener exactly once. ---

func TestOpenBuildsSetupURLFromDefaultAddr(t *testing.T) {
	o := &recordingOpener{}
	if err := Open(context.Background(), ":8080", o); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(o.urls) != 1 || o.urls[0] != "http://localhost:8080/setup" {
		t.Errorf("opener called with %v, want exactly [http://localhost:8080/setup]", o.urls)
	}
}

func TestOpenWithFullAddr(t *testing.T) {
	o := &recordingOpener{}
	if err := Open(context.Background(), "glyphux.example:9090", o); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(o.urls) != 1 || o.urls[0] != "http://glyphux.example:9090/setup" {
		t.Errorf("opener called with %v, want exactly [http://glyphux.example:9090/setup]", o.urls)
	}
}

// --- Acceptance criterion: a failing opener's error propagates (the CLI
// reports it instead of swallowing it). ---

func TestOpenPropagatesOpenerError(t *testing.T) {
	if err := Open(context.Background(), ":8080", errOpener{}); err == nil {
		t.Fatal("Open must propagate the opener error")
	}
}
