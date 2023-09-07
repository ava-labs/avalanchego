// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime/subprocess"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

var _ vms.Factory = (*factory)(nil)

type factory struct {
	path           string
	processTracker resource.ProcessTracker
	runtimeTracker runtime.Tracker
}

func NewFactory(path string, processTracker resource.ProcessTracker, runtimeTracker runtime.Tracker) vms.Factory {
	return &factory{
		path:           path,
		processTracker: processTracker,
		runtimeTracker: runtimeTracker,
	}
}

func (f *factory) New(log logging.Logger) (interface{}, error) {
	config := &subprocess.Config{
		Stderr:           log,
		Stdout:           log,
		HandshakeTimeout: runtime.DefaultHandshakeTimeout,
		Log:              log,
	}

	listener, err := grpcutils.NewListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	status, stopper, err := subprocess.Bootstrap(
		context.TODO(),
		listener,
		subprocess.NewCmd(f.path),
		config,
	)
	if err != nil {
		return nil, err
	}

	vmConn, err := grpcutils.Dial(status.Addr)
	if err != nil {
		return nil, err
	}

	vm := NewClient(vmpb.NewVMClient(vmConn))

	healthDialOpts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(false), // fail fast
		),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	// gRPC health check connection will fail fast if the server is not ready.
	// this is useful for detecting if the VM process has crashed.
	healthConn, err := grpc.Dial(status.Addr, healthDialOpts...)
	if err != nil {
		return nil, err
	}
	// gRPC health check client
	// ref. https://github.com/grpc/grpc/blob/master/doc/health-checking.md
	vm.healthCli = healthpb.NewHealthClient(healthConn)

	vm.SetProcess(stopper, status.Pid, f.processTracker)

	f.runtimeTracker.TrackRuntime(stopper)

	return vm, nil
}
