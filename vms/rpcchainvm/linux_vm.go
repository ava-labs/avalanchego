// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build linux
// +build linux

// ^ SIGUSR1 signal is not available on Windows

package rpcchainvm

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gruntime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"

	runtimepb "github.com/ava-labs/avalanchego/proto/pb/vm/runtime"
)

func serve(ctx context.Context, vm block.ChainVM, opts ...grpcutils.ServerOption) error {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	server := newVMServer(vm, opts...)
	go func(ctx context.Context) {
		defer func() {
			server.GracefulStop()
			fmt.Println("vm server: graceful termination success")
		}()

		for {
			select {
			case s := <-signals:
				switch s {
				case syscall.SIGINT, syscall.SIGTERM:
					// these signals are ignored because ctrl-c will send SIGINT
					// to all processes and systemd will send SIGTERM to all
					// processes by default during stop.
					fmt.Printf("runtime engine: ignoring signal: %s\n", s)
				case syscall.SIGUSR1:
					fmt.Printf("runtime engine: received shutdown signal: %s\n", s)
					return
				}
			case <-ctx.Done():
				fmt.Println("runtime engine: context has been cancelled")
				return
			}
		}
	}(ctx)

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

	// start RPC Chain VM server
	grpcutils.Serve(listener, server)

	return nil
}
