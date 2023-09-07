// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gruntime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	runtimepb "github.com/ava-labs/avalanchego/proto/pb/vm/runtime"
)

const defaultRuntimeDialTimeout = 5 * time.Second

// The address of the Runtime server is expected to be passed via ENV `runtime.EngineAddressKey`.
// This address is used by the Runtime client to send Initialize RPC to server.
//
// Serve starts the RPC Chain VM server and performs a handshake with the VM runtime service.
func Serve(ctx context.Context, vm block.ChainVM, opts ...grpcutils.ServerOption) error {
	ctx, shutdownCtxCancel := context.WithCancel(ctx)
	server := newVMServer(vm, shutdownCtxCancel, opts...)
	go func(ctx context.Context) {
		err := startVMServer(ctx, server)
		if err != nil {
			fmt.Printf("runtime engine: failed to start VM server: %s\n", err)
		}
	}(ctx)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	for {
		select {
		case s := <-signals:
			switch s {
			case syscall.SIGINT, syscall.SIGTERM:
				// these signals are ignored because ctrl-c will send SIGINT
				// to all processes and systemd will send SIGTERM to all
				// processes by default during stop.
				fmt.Printf("runtime engine: ignoring signal: %s\n", s)
			}
		case <-ctx.Done():
			fmt.Println("runtime engine: attempting graceful shutdown")
			// attempt graceful shutdown of VM server
			ctx, cancel := context.WithTimeout(context.Background(), runtime.DefaultGracefulTimeout)
			defer cancel()
			if err := stopVMServer(ctx, server); err != nil {
				fmt.Printf("runtime engine: %v\n", err)
			} else {
				fmt.Println("runtime engine: VM server shutdown successfully")
			}
			return nil
		}
	}
}

// Returns an RPC Chain VM server serving health and VM services.
func newVMServer(vm block.ChainVM, shutdownCtxCancel context.CancelFunc, opts ...grpcutils.ServerOption) *grpc.Server {
	server := grpcutils.NewServer(opts...)
	vmpb.RegisterVMServer(server, NewServer(vm, shutdownCtxCancel))

	health := health.NewServer()
	health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, health)

	return server
}

// startVMServer starts the RPC Chain VM server and performs a handshake with the VM runtime service.
func startVMServer(ctx context.Context, server *grpc.Server) error {
	listener, err := grpcutils.NewListener()
	if err != nil {
		return fmt.Errorf("failed to create new listener: %w", err)
	}

	vmAddr := listener.Addr().String()
	if err := initialize(ctx, vmAddr); err != nil {
		_ = listener.Close()
		return err
	}

	grpcutils.Serve(listener, server)
	return nil
}

func stopVMServer(ctx context.Context, server *grpc.Server) error {
	stop := make(chan struct{})
	go func() {
		defer close(stop)
		// block until all pending RPCs are finished
		server.GracefulStop()
	}()

	select {
	case <-stop:
		return nil
	case <-ctx.Done():
		// force shutdown after timeout
		server.Stop()
		return fmt.Errorf("failed to gracefully shutdown VM server: %w", ctx.Err())
	}
}

// initialize performs a handshake with the VM runtime service by sending the
// address of the VM server to the RPC ChainVM Client.
func initialize(ctx context.Context, vmAddr string) error {
	// address of Runtime server is passed via ENV
	runtimeAddr := os.Getenv(runtime.EngineAddressKey)
	if runtimeAddr == "" {
		return fmt.Errorf("required env var missing: %q", runtime.EngineAddressKey)
	}

	clientConn, err := grpcutils.Dial(runtimeAddr)
	if err != nil {
		return fmt.Errorf("failed to create client conn: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultRuntimeDialTimeout)
	defer cancel()

	// send Initialize RPC to Runtime server providing the address of the VM server.
	client := gruntime.NewClient(runtimepb.NewRuntimeClient(clientConn))
	err = client.Initialize(ctx, version.RPCChainVMProtocol, vmAddr)
	if err != nil {
		return fmt.Errorf("failed to initialize vm runtime: %w", err)
	}
	return nil
}
