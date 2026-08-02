// Ticket T10b RED: the CLI dispatch contract — `glyphux service
// install|status` reaches the manager, `glyphux instance open` reaches the
// opener, and an unknown service subcommand is a usage error. Driven
// through runWith(deps) with fake executors/openers — no real systemd.
package main

import (
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/service"
)

type recordingExec struct {
	calls []string
	out   string
	err   error
}

func (r *recordingExec) Run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return []byte(r.out), r.err
}

type recordingOpener struct{ urls []string }

func (r *recordingOpener) Open(u string) error {
	r.urls = append(r.urls, u)
	return nil
}

func TestServiceInstallDispatchesToManager(t *testing.T) {
	fe := &recordingExec{}
	d := deps{service: service.NewManager(fe, service.WithUnitDir(t.TempDir()), service.WithPlatform("linux"))}

	if err := runWith([]string{"service", "install"}, d); err != nil {
		t.Fatalf("runWith(service install): %v", err)
	}
	if len(fe.calls) == 0 || fe.calls[0] != "systemctl enable --now glyphux.service" {
		t.Errorf("exec calls = %v, want systemctl enable --now glyphux.service", fe.calls)
	}
}

func TestServiceStatusDispatchesToManager(t *testing.T) {
	fe := &recordingExec{out: "active\n"}
	d := deps{service: service.NewManager(fe, service.WithPlatform("linux"))}

	if err := runWith([]string{"service", "status"}, d); err != nil {
		t.Fatalf("runWith(service status): %v", err)
	}
	if len(fe.calls) != 1 || fe.calls[0] != "systemctl is-active glyphux.service" {
		t.Errorf("exec calls = %v, want [systemctl is-active glyphux.service]", fe.calls)
	}
}

func TestServiceUnknownSubcommandIsUsageError(t *testing.T) {
	err := runWith([]string{"service", "bogus"}, deps{})
	if err == nil {
		t.Fatal("runWith(service bogus) must return an error")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("error %q must cite usage", err)
	}
}

func TestInstanceOpenDispatchesToOpener(t *testing.T) {
	o := &recordingOpener{}
	d := deps{addr: ":8080", opener: o}

	if err := runWith([]string{"instance", "open"}, d); err != nil {
		t.Fatalf("runWith(instance open): %v", err)
	}
	if len(o.urls) != 1 || o.urls[0] != "http://localhost:8080/setup" {
		t.Errorf("opener called with %v, want exactly [http://localhost:8080/setup]", o.urls)
	}
}
