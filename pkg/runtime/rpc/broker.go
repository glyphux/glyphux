package rpc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/glyphux/glyphux/pkg/runtime/rpc/rpcpb"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// State is a Broker's observable lifecycle state (PRD §8.1's "process
// supervision, crash isolation" requirement made concrete: a caller must be
// able to ask "is this plugin still alive?" and get a real answer).
type State int32

const (
	// StateStarting: the subprocess has been launched but has not yet
	// signaled that its gRPC server is listening.
	StateStarting State = iota
	// StateRunning: the subprocess signaled it is ready and is presumed
	// alive (no exit observed yet).
	StateRunning
	// StateDead: the subprocess has exited (cleanly, crashed, or was
	// killed) — observed via a real os/exec.Cmd.Wait() return, not
	// inferred or simulated. Terminal; a Broker never leaves this state.
	StateDead
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateDead:
		return "dead"
	default:
		return "unknown"
	}
}

// ErrPluginNotReady is returned by Launch when the subprocess exited (or the
// ready timeout elapsed) before it signaled readiness.
var ErrPluginNotReady = errors.New("rpc: plugin subprocess did not become ready")

// readyLine is the exact line a plugin subprocess must write to its stdout,
// once and only once, to signal that its Plugin gRPC server is listening on
// GLYPHUX_PLUGIN_SOCK. This is a deliberately dumb, synchronous handshake —
// no fixed sleeps, no polling loop guessing when the subprocess is ready;
// the host actually waits on this exact signal. See pkg/runtime/rpc/testdata/fixtureplugin
// for the reference implementation of the other side of this contract.
const readyLine = "GLYPHUX_RPC_PLUGIN_READY"

// ConsentChecker is the seam slice 2.7's install-time consent engine plugs
// into. PRD §10.2 names three distinct consent points (marketplace review,
// install-time consent, runtime enforcement); this slice only builds the
// third (runtime enforcement, via the sdk.HostAPI the Broker exposes,
// exactly as slice 2.1/2.6 already gate it by manifest declaration). Nothing
// in this package calls ConsentChecker — "declared in the manifest" is
// treated as "consented" for now, matching pkg/sdk's own existing
// NewHostAPI behavior (see pkg/sdk/host.go). A future version of Broker (or
// a wrapper around it) is expected to consult a ConsentChecker — most
// naturally by having the caller pass an sdk.HostAPI that was itself built
// from a consent-filtered manifest — before Launch exposes it to a
// subprocess. Defined here, now, purely so that seam has a name and a
// documented shape for slice 2.7 to target.
type ConsentChecker interface {
	// Allowed reports whether pluginName has been granted capability/scope
	// by whatever mechanism slice 2.7 implements (installer consent record,
	// policy store, etc).
	Allowed(pluginName, capability, scope string) bool
}

// Config is a Broker's launch configuration.
type Config struct {
	// Command is the plugin subprocess's executable path.
	Command string
	// Args are passed to the subprocess after Command.
	Args []string
	// Env is appended to the subprocess's environment (in addition to
	// os.Environ() and the GLYPHUX_HOST_SOCK/GLYPHUX_PLUGIN_SOCK vars this
	// package sets itself).
	Env []string
	// NetworkAllowlist is the exact set of outbound hosts this plugin's
	// consent granted — the "network" permission's Args from the
	// consent-filtered manifest (sdk.FilterManifest over the granted
	// subset). Launch sets it into the subprocess environment as
	// GLYPHUX_NETWORK_ALLOWLIST (comma-joined, always present: an empty
	// list is set as the empty string, the explicit "deny all outbound"
	// state a subprocess can distinguish from a launcher that never set
	// the variable).
	NetworkAllowlist []string
	// OutboundProxyURL, if non-empty, is the operator-configured egress
	// proxy (config rpc_outbound_proxy_url) injected into the subprocess
	// environment as HTTP_PROXY and HTTPS_PROXY — the choke point for a
	// Tier-C plugin's own outbound connections, which the host cannot
	// intercept for it. Empty leaves proxy variables untouched (inherited
	// from the daemon's own environment).
	OutboundProxyURL string
	// HostAPI is the already capability-gated sdk.HostAPI (built via
	// sdk.NewHostAPI against the plugin's manifest) exposed to the
	// subprocess over gRPC. Required.
	HostAPI sdk.HostAPI
	// ReadyTimeout bounds how long Launch waits for the subprocess to
	// signal readiness before giving up. Defaults to 5s if zero.
	ReadyTimeout time.Duration
}

// Broker launches one plugin subprocess, exposes its HostAPI over a gRPC
// connection carried on a Unix domain socket, and supervises the
// subprocess's lifetime. One Broker manages exactly one subprocess.
//
// Transport choice: Unix domain sockets, not loopback TCP. Rationale: a UDS
// path lives in a per-Broker temp directory no other process on the machine
// can guess, so two Brokers (or an unrelated local process) can never
// collide on a port; there is no bind-race against anything else using
// loopback TCP; and the socket file's filesystem permissions are an extra,
// free access-control layer loopback TCP doesn't offer. The cost (no
// portability to a remote-out-of-process host) is accepted because PRD
// §8.1's Tier C is "own process," not "own machine" — nothing in the PRD
// asks for the plugin to run anywhere but alongside the host.
type Broker struct {
	cmd *exec.Cmd

	hostSrv     *grpc.Server
	hostLis     net.Listener
	pluginSock  string
	dir         string
	pluginConn  *grpc.ClientConn
	pluginStub  rpcpb.PluginClient

	state atomic.Int32

	doneCh  chan struct{}
	doneErr error

	mu     sync.Mutex
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// Launch starts the subprocess described by cfg, stands up the gRPC HostAPI
// server it will connect back to, and blocks until the subprocess either
// signals readiness, exits, or cfg.ReadyTimeout elapses. It always returns a
// non-nil *Broker (even on error) so a caller can inspect captured
// stdout/stderr and State() for diagnostics; err is non-nil exactly when the
// subprocess is not usable.
func Launch(cfg Config) (*Broker, error) {
	if cfg.HostAPI == nil {
		return nil, errors.New("rpc: Config.HostAPI is required")
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 5 * time.Second
	}

	dir, err := os.MkdirTemp("", "glyphux-rpc-broker-*")
	if err != nil {
		return nil, fmt.Errorf("rpc: create socket dir: %w", err)
	}
	hostSock := filepath.Join(dir, "host.sock")
	pluginSock := filepath.Join(dir, "plugin.sock")

	hostLis, err := net.Listen("unix", hostSock)
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("rpc: listen for host service: %w", err)
	}

	hostSrv := grpc.NewServer()
	rpcpb.RegisterHostAPIServer(hostSrv, newHostServer(cfg.HostAPI))
	go hostSrv.Serve(hostLis) //nolint:errcheck // Stop()/lis.Close() on shutdown paths below

	b := &Broker{
		hostSrv:    hostSrv,
		hostLis:    hostLis,
		pluginSock: pluginSock,
		dir:        dir,
		doneCh:     make(chan struct{}),
	}
	b.state.Store(int32(StateStarting))

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = append(append([]string{}, os.Environ()...), cfg.Env...)
	cmd.Env = append(cmd.Env,
		"GLYPHUX_HOST_SOCK="+hostSock,
		"GLYPHUX_PLUGIN_SOCK="+pluginSock,
		// The granted outbound allowlist, always present: empty string =
		// explicit deny-all (Ticket T3 / gap 5). Appended last so it wins
		// over any same-named variable inherited from os.Environ().
		"GLYPHUX_NETWORK_ALLOWLIST="+strings.Join(cfg.NetworkAllowlist, ","),
	)
	// Operator egress proxy (rpc_outbound_proxy_url): injected only when
	// configured; otherwise the subprocess inherits the daemon's own
	// proxy environment untouched.
	if cfg.OutboundProxyURL != "" {
		cmd.Env = append(cmd.Env,
			"HTTP_PROXY="+cfg.OutboundProxyURL,
			"HTTPS_PROXY="+cfg.OutboundProxyURL,
		)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		b.teardownListener()
		os.RemoveAll(dir)
		return b, fmt.Errorf("rpc: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		b.teardownListener()
		os.RemoveAll(dir)
		return b, fmt.Errorf("rpc: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		b.teardownListener()
		os.RemoveAll(dir)
		return b, fmt.Errorf("rpc: start subprocess: %w", err)
	}
	b.cmd = cmd

	readyCh := make(chan struct{})
	var readyOnce sync.Once

	go b.drainReader(stderrPipe, &b.stderr, nil, nil)
	go b.drainReader(stdoutPipe, &b.stdout, readyCh, &readyOnce)

	go func() {
		waitErr := cmd.Wait()
		b.mu.Lock()
		b.doneErr = waitErr
		b.mu.Unlock()
		b.state.Store(int32(StateDead))
		close(b.doneCh)
		b.hostSrv.Stop()
		b.hostLis.Close()
	}()

	select {
	case <-readyCh:
		b.state.Store(int32(StateRunning))
		return b, nil
	case <-b.doneCh:
		return b, fmt.Errorf("%w: %v (stderr: %s)", ErrPluginNotReady, b.doneErr, b.Stderr())
	case <-time.After(cfg.ReadyTimeout):
		b.Close()
		return b, fmt.Errorf("%w: timed out after %s", ErrPluginNotReady, cfg.ReadyTimeout)
	}
}

// drainReader copies r line-by-line into buf (under b.mu), and — if
// readyCh/once are non-nil — closes readyCh exactly once upon seeing
// readyLine. It always drains to EOF so the subprocess never blocks on a
// full pipe buffer.
func (b *Broker) drainReader(r io.Reader, buf *bytes.Buffer, readyCh chan struct{}, once *sync.Once) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		b.mu.Lock()
		buf.WriteString(line)
		buf.WriteByte('\n')
		b.mu.Unlock()
		if readyCh != nil && line == readyLine {
			once.Do(func() { close(readyCh) })
		}
	}
}

func (b *Broker) teardownListener() {
	b.hostSrv.Stop()
	b.hostLis.Close()
}

// State reports the Broker's current lifecycle state.
func (b *Broker) State() State {
	return State(b.state.Load())
}

// Done returns a channel closed exactly once the subprocess has exited,
// however it exited.
func (b *Broker) Done() <-chan struct{} {
	return b.doneCh
}

// Wait blocks until the subprocess exits and returns the underlying
// os/exec.Cmd.Wait() error (nil for a clean exit(0), a *exec.ExitError for
// any other exit code or a signal death).
func (b *Broker) Wait() error {
	<-b.doneCh
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.doneErr
}

// Stdout returns everything captured from the subprocess's stdout so far.
func (b *Broker) Stdout() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.String()
}

// Stderr returns everything captured from the subprocess's stderr so far.
func (b *Broker) Stderr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stderr.String()
}

// dialPlugin lazily dials the subprocess's Plugin gRPC service the first
// time it's needed (Register), reusing the connection afterward.
func (b *Broker) dialPlugin(ctx context.Context) (rpcpb.PluginClient, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pluginStub != nil {
		return b.pluginStub, nil
	}
	conn, err := grpc.NewClient(
		"unix:"+b.pluginSock,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("rpc: dial plugin: %w", err)
	}
	b.pluginConn = conn
	b.pluginStub = rpcpb.NewPluginClient(conn)
	return b.pluginStub, nil
}

// Register calls the subprocess's Plugin.Register RPC with mode (and an
// optional mode-specific arg), and returns its response. This is the one
// call this slice's Broker makes INTO the plugin subprocess (the reverse
// direction of every HostAPI RPC above) — mirroring the conceptual
// Plugin.Register(host HostAPI) entry point PRD §8.3 describes for every
// tier.
func (b *Broker) Register(ctx context.Context, mode string, arg ...string) (*rpcpb.RegisterResponse, error) {
	stub, err := b.dialPlugin(ctx)
	if err != nil {
		return nil, err
	}
	var a string
	if len(arg) > 0 {
		a = arg[0]
	}
	return stub.Register(ctx, &rpcpb.RegisterRequest{Mode: mode, Arg: a})
}

// Close stops the host gRPC server, closes any plugin client connection,
// and — if the subprocess is still alive — kills it. Removes the Broker's
// temporary socket directory. Safe to call after the subprocess has already
// exited on its own (the common case in the crash tests).
func (b *Broker) Close() error {
	b.mu.Lock()
	if b.pluginConn != nil {
		b.pluginConn.Close()
		b.pluginConn = nil
		b.pluginStub = nil
	}
	b.mu.Unlock()

	if b.cmd == nil {
		// Launch never reached cmd.Start() (an earlier setup step failed);
		// there is no subprocess-Wait goroutine to close doneCh, so don't
		// block on it.
		b.teardownListener()
		os.RemoveAll(b.dir)
		return nil
	}
	if b.cmd.Process != nil && b.State() != StateDead {
		_ = b.cmd.Process.Kill()
	}
	<-b.doneCh // drained by the Wait goroutine, which also stops hostSrv/hostLis
	os.RemoveAll(b.dir)
	return nil
}
