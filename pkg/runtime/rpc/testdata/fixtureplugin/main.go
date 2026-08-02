// Command fixtureplugin is a real, separately-compiled Tier-C test plugin
// used ONLY by pkg/runtime/rpc's tests to prove the broker end to end: a
// genuine OS subprocess that dials back into the host's HostAPI gRPC
// service and serves its own Plugin gRPC service for the host to call.
//
// Environment contract (set by pkg/runtime/rpc.Broker.Launch):
//   - GLYPHUX_HOST_SOCK: unix socket path to dial for the host's HostAPI service.
//   - GLYPHUX_PLUGIN_SOCK: unix socket path this process must listen on for
//     its own Plugin service.
//
// Optional environment:
//   - GLYPHUX_FIXTURE_CRASH_BEFORE_READY=1: exit(1) immediately, before
//     listening or printing the ready line at all — simulates a plugin that
//     crashes during its own startup, before the host ever gets a working
//     connection.
//
// Once listening, this process prints the exact line "GLYPHUX_RPC_PLUGIN_READY"
// to stdout (the handshake pkg/runtime/rpc.Broker.Launch waits on) and then
// serves Plugin.Register requests. Each Register call's Mode selects what
// this fixture does on that call, dialing back into GLYPHUX_HOST_SOCK to
// exercise a real HostAPI RPC:
//
//   - "store-roundtrip": Store.Set then Store.Get, asserting round-trip
//     equality, logging PASS/FAIL.
//   - "emit": calls HostAPI.Emit once; logs whether it succeeded or the
//     error it got back (the caller decides whether that was expected,
//     since this fixture doesn't know the manifest's declared scopes).
//   - "network-check": calls HostAPI.AllowsNetworkHost for a fixed host and
//     logs the answer.
//   - "env-allowlist": echoes GLYPHUX_NETWORK_ALLOWLIST (log[0]) — the
//     subprocess's own view of its outbound policy, set by the broker.
//   - "env-proxy": echoes HTTP_PROXY (log[0]) and HTTPS_PROXY (log[1]) —
//     the operator egress proxy the broker injected, if any.
//   - "crash": calls os.Exit(1) from inside the RPC handler, before ever
//     responding — simulates a plugin crashing mid-operation. The host's
//     Register call will observe a transport error; separately, the
//     Broker's process-supervision goroutine observes the real process exit.
package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/glyphux/glyphux/pkg/runtime/rpc/rpcpb"
)

func main() {
	if os.Getenv("GLYPHUX_FIXTURE_CRASH_BEFORE_READY") == "1" {
		os.Exit(1)
	}

	hostSock := mustEnv("GLYPHUX_HOST_SOCK")
	pluginSock := mustEnv("GLYPHUX_PLUGIN_SOCK")

	lis, err := net.Listen("unix", pluginSock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixtureplugin: listen:", err)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	rpcpb.RegisterPluginServer(srv, &fixture{hostSock: hostSock})

	stdout := bufio.NewWriter(os.Stdout)
	fmt.Fprintln(stdout, "GLYPHUX_RPC_PLUGIN_READY")
	stdout.Flush()

	if err := srv.Serve(lis); err != nil {
		fmt.Fprintln(os.Stderr, "fixtureplugin: serve:", err)
		os.Exit(1)
	}
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		fmt.Fprintln(os.Stderr, "fixtureplugin: missing required env var", name)
		os.Exit(1)
	}
	return v
}

type fixture struct {
	rpcpb.UnimplementedPluginServer
	hostSock string
}

func (f *fixture) dialHost(ctx context.Context) (rpcpb.HostAPIClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient("unix:"+f.hostSock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return rpcpb.NewHostAPIClient(conn), conn, nil
}

func (f *fixture) Register(ctx context.Context, req *rpcpb.RegisterRequest) (*rpcpb.RegisterResponse, error) {
	switch req.GetMode() {
	case "crash":
		// Deliberately crash from inside the handler, before responding —
		// exercises the host's mid-call crash-detection path for real.
		os.Exit(1)
		return nil, nil // unreachable

	case "store-roundtrip":
		client, conn, err := f.dialHost(ctx)
		if err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: err.Error()}, nil
		}
		defer conn.Close()

		const key, want = "greeting", "hello-from-plugin"
		if _, err := client.StoreSet(ctx, &rpcpb.StoreSetRequest{Key: key, Value: []byte(want)}); err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: "StoreSet: " + err.Error()}, nil
		}
		got, err := client.StoreGet(ctx, &rpcpb.StoreGetRequest{Key: key})
		if err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: "StoreGet: " + err.Error()}, nil
		}
		if !got.GetOk() || string(got.GetValue()) != want {
			return &rpcpb.RegisterResponse{Ok: false, Error: fmt.Sprintf("round trip mismatch: got %q ok=%v, want %q", got.GetValue(), got.GetOk(), want)}, nil
		}
		return &rpcpb.RegisterResponse{Ok: true, Log: []string{"store round trip matched"}}, nil

	case "emit":
		client, conn, err := f.dialHost(ctx)
		if err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: err.Error()}, nil
		}
		defer conn.Close()

		_, err = client.Emit(ctx, &rpcpb.EmitRequest{Event: "plugin.ping", Payload: []byte("hi")})
		if err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: err.Error()}, nil
		}
		return &rpcpb.RegisterResponse{Ok: true, Log: []string{"emit succeeded"}}, nil

	case "network-check":
		client, conn, err := f.dialHost(ctx)
		if err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: err.Error()}, nil
		}
		defer conn.Close()

		resp, err := client.AllowsNetworkHost(ctx, &rpcpb.AllowsNetworkHostRequest{Host: req.GetArg()})
		if err != nil {
			return &rpcpb.RegisterResponse{Ok: false, Error: err.Error()}, nil
		}
		return &rpcpb.RegisterResponse{Ok: resp.GetAllowed(), Log: []string{fmt.Sprintf("allowed=%v", resp.GetAllowed())}}, nil

	case "env-allowlist":
		return &rpcpb.RegisterResponse{Ok: true, Log: []string{os.Getenv("GLYPHUX_NETWORK_ALLOWLIST")}}, nil

	case "env-proxy":
		return &rpcpb.RegisterResponse{Ok: true, Log: []string{os.Getenv("HTTP_PROXY"), os.Getenv("HTTPS_PROXY")}}, nil

	default:
		return &rpcpb.RegisterResponse{Ok: false, Error: "unknown mode: " + req.GetMode()}, nil
	}
}
