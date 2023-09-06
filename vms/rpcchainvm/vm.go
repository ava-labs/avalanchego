package rpcchainvm

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
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
