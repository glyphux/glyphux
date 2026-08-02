// Command glyphux is the optional developer CLI — a secondary surface for
// scripting, automation, and CI (§6.1). It is never the install path; the
// daemon's web wizard is. The T10b service/instance commands dispatch
// through runWith(deps) — the injectable seam tests drive with fakes.
package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/glyphux/glyphux/internal/instance"
	"github.com/glyphux/glyphux/internal/service"
)

const serviceUsage = `Usage: glyphux service <install|uninstall|status|restart>

  install    Install and enable the glyphux system service (Linux: systemd)
  uninstall  Stop, disable, and remove the service
  status     Report the service lifecycle state
  restart    Restart the service
`

const instanceUsage = `Usage: glyphux instance open

  open    Open the daemon's setup wizard in the browser
`

// deps carries the injectable collaborators the service/instance commands
// dispatch to — a real service.Manager and browser opener in production,
// fakes in tests.
type deps struct {
	addr    string
	service *service.Manager
	opener  instance.Opener
}

// defaultDeps builds the production deps: the config default listen
// address, a manager over the host platform driving a real executor, and
// the platform browser opener.
func defaultDeps() deps {
	return deps{
		addr:    ":8080",
		service: service.NewManager(realExec{}),
		opener:  browserOpener{},
	}
}

// runWith dispatches args against d — the testable seam behind run().
// Everything run() already handles (version, composition) falls through to
// it unchanged; service and instance are the T10b additions.
func runWith(args []string, d deps) error {
	if len(args) == 0 {
		return run(args)
	}
	switch args[0] {
	case "service":
		return runService(args[1:], d)
	case "instance":
		return runInstance(args[1:], d)
	default:
		return run(args)
	}
}

// runService dispatches the service subcommands to the manager. A nil
// manager (test deps that omit it) falls back to a real one.
func runService(args []string, d deps) error {
	m := d.service
	if m == nil {
		m = service.NewManager(realExec{})
	}
	if len(args) == 0 {
		fmt.Print(serviceUsage)
		return fmt.Errorf("usage: glyphux service <install|uninstall|status|restart>")
	}
	ctx := context.Background()
	switch args[0] {
	case "install":
		return m.Install(ctx)
	case "uninstall":
		return m.Uninstall(ctx)
	case "status":
		s, err := m.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("glyphux service: %s\n", s)
		return nil
	case "restart":
		return m.Restart(ctx)
	default:
		fmt.Print(serviceUsage)
		return fmt.Errorf("unknown service subcommand %q (usage: glyphux service <install|uninstall|status|restart>)", args[0])
	}
}

// runInstance dispatches the instance subcommands. A nil opener (test deps
// that omit it) falls back to the platform browser opener.
func runInstance(args []string, d deps) error {
	if len(args) == 1 && args[0] == "open" {
		opener := d.opener
		if opener == nil {
			opener = browserOpener{}
		}
		return instance.Open(context.Background(), d.addr, opener)
	}
	fmt.Print(instanceUsage)
	return fmt.Errorf("usage: glyphux instance open")
}

// realExec is the production Executor: it runs the command and returns its
// combined output.
type realExec struct{}

func (realExec) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// browserOpener is the production Opener: the platform's browser launcher.
type browserOpener struct{}

func (browserOpener) Open(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}
