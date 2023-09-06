package rpcchainvm

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gruntime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	runtimepb "github.com/ava-labs/avalanchego/proto/pb/vm/runtime"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const defaultRuntimeDialTimeout = 5 * time.Second

// The address of the Runtime server is expected to be passed via ENV `runtime.EngineAddressKey`.
// This address is used by the Runtime client to send Initialize RPC to server.
//
// Serve starts the RPC Chain VM server and performs a handshake with the VM runtime service.
func Serve(ctx context.Context, vm block.ChainVM, opts ...grpcutils.ServerOption) error {
	return serve(ctx, vm, opts...)
}

// Returns an RPC Chain VM server serving health and VM services.
func newVMServer(vm block.ChainVM, opts ...grpcutils.ServerOption) *grpc.Server {
	server := grpcutils.NewServer(opts...)
	vmpb.RegisterVMServer(server, NewServer(vm))

	health := health.NewServer()
	health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, health)

	return server
}

func startVMServer(ctx context.Context, server *grpc.Server) error {
	// address of Runtime server from ENV
	runtimeAddr := os.Getenv(runtime.EngineAddressKey)
	if runtimeAddr == "" {
		return fmt.Errorf("required env var missing: %q", runtime.EngineAddressKey)
	}

	clientConn, err := grpcutils.Dial(runtimeAddr)
	if err != nil {
		return fmt.Errorf("failed to create client conn: %w", err)
	}

	client := gruntime.NewClient(runtimepb.NewRuntimeClient(clientConn))

	listener, err := grpcutils.NewListener()
	if err != nil {
		return fmt.Errorf("failed to create new listener: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultRuntimeDialTimeout)
	defer cancel()
	err = client.Initialize(ctx, version.RPCChainVMProtocol, listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to initialize vm runtime: %w", err)
	}

	grpcutils.Serve(listener, server)
	return nil
}
