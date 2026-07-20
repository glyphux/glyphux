// Package rpc is the Tier-C (PRD §8.1) plugin runtime: a gRPC broker that
// launches a plugin as a real OS subprocess, exposes a manifest-gated
// pkg/sdk.HostAPI to it over a real gRPC connection, and supervises the
// subprocess for crash isolation. See docs/implementation/active/0016 for
// the full design record.
package rpc

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/glyphux/glyphux/pkg/runtime/rpc/rpcpb"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// hostServer implements rpcpb.HostAPIServer by delegating every RPC to a
// real sdk.HostAPI value. It adds NO gating logic of its own — every
// capability check (does this plugin's manifest declare events:emit? does
// it declare the "network" permission for this host?) already lives in
// pkg/sdk (NewHostAPI / hostAPI's hasScope checks), which is exactly PRD
// §10.3's point: "the boundary... [is] where a plugin is stripped of
// anything it did not declare" — this type is the boundary's TRANSPORT, the
// stripping itself happens one layer down, in code this slice does not
// modify. This is what makes the deny-by-default behavior verifiable end to
// end: build two sdk.HostAPI values from two different manifests (one
// declaring events:emit, one not), wrap each in a hostServer, and the exact
// same RPC call succeeds against one and errors against the other.
type hostServer struct {
	rpcpb.UnimplementedHostAPIServer
	api sdk.HostAPI
}

func newHostServer(api sdk.HostAPI) *hostServer {
	return &hostServer{api: api}
}

func (s *hostServer) StoreGet(ctx context.Context, req *rpcpb.StoreGetRequest) (*rpcpb.StoreGetResponse, error) {
	value, ok, err := s.api.Store().Get(ctx, req.GetKey())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &rpcpb.StoreGetResponse{Value: value, Ok: ok}, nil
}

func (s *hostServer) StoreSet(ctx context.Context, req *rpcpb.StoreSetRequest) (*rpcpb.StoreSetResponse, error) {
	if err := s.api.Store().Set(ctx, req.GetKey(), req.GetValue()); err != nil {
		return nil, toGRPCError(err)
	}
	return &rpcpb.StoreSetResponse{}, nil
}

func (s *hostServer) StoreDelete(ctx context.Context, req *rpcpb.StoreDeleteRequest) (*rpcpb.StoreDeleteResponse, error) {
	if err := s.api.Store().Delete(ctx, req.GetKey()); err != nil {
		return nil, toGRPCError(err)
	}
	return &rpcpb.StoreDeleteResponse{}, nil
}

// Emit forwards to sdk.HostAPI.Emit, which is gated on the manifest's
// "events:emit" api scope (pkg/sdk/host.go's hasScope check). A plugin
// subprocess whose manifest didn't declare it gets a real gRPC error here
// (status code FailedPrecondition wrapping sdk.ErrScopeNotDeclared) — the
// RPC method is reachable (the server registers it unconditionally, since
// gRPC has no per-call method-hiding primitive), but calling it for an
// undeclared capability is rejected at the sdk.HostAPI layer every time,
// which is the deny-by-default property this slice must prove for real.
func (s *hostServer) Emit(ctx context.Context, req *rpcpb.EmitRequest) (*rpcpb.EmitResponse, error) {
	if err := s.api.Emit(ctx, req.GetEvent(), req.GetPayload()); err != nil {
		return nil, toGRPCError(err)
	}
	return &rpcpb.EmitResponse{}, nil
}

// AllowsNetworkHost forwards to sdk.HostAPI.AllowsNetworkHost — the
// "allowlisted network" decision primitive (PRD §10.4). This slice wires it
// up as a callable query the plugin subprocess can make of the host before
// making its own outbound network call; it deliberately does NOT intercept
// the subprocess's actual OS-level network traffic (see this slice's
// tracking doc for why that is out of scope for Tier C).
func (s *hostServer) AllowsNetworkHost(ctx context.Context, req *rpcpb.AllowsNetworkHostRequest) (*rpcpb.AllowsNetworkHostResponse, error) {
	return &rpcpb.AllowsNetworkHostResponse{Allowed: s.api.AllowsNetworkHost(req.GetHost())}, nil
}

// toGRPCError maps an sdk-layer error to a gRPC status error so it survives
// the process boundary as an inspectable code, not just a flattened string.
// A capability/permission-gating denial (sdk.ErrScopeNotDeclared) becomes
// FailedPrecondition; anything else becomes Unknown.
//
// Detection is via strings.Contains on the sentinel's own message, not
// errors.Is: pkg/sdk's gating methods (see pkg/sdk/host.go, e.g. Emit's
// `errors.New("events:emit: " + ErrScopeNotDeclared.Error())`) build their
// returned error via string concatenation, not `%w` wrapping, so the
// sentinel is not part of the error chain errors.Is walks. This package
// does not modify pkg/sdk (out of scope for this slice), so it adapts to
// that error shape here rather than upstream.
func toGRPCError(err error) error {
	if strings.Contains(err.Error(), sdk.ErrScopeNotDeclared.Error()) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Unknown, err.Error())
}
