// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime/subprocess"
)

var _ vms.Factory = (*factory)(nil)

type factory struct {
	path             string
	processTracker   resource.ProcessTracker
	runtimeTracker   runtime.Tracker
	handshakeTimeout time.Duration
}

func NewFactory(
	path string,
	processTracker resource.ProcessTracker,
	runtimeTracker runtime.Tracker,
	handshakeTimeout time.Duration,
) vms.Factory {
	return &factory{
		path:             path,
		processTracker:   processTracker,
		runtimeTracker:   runtimeTracker,
		handshakeTimeout: handshakeTimeout,
	}
}

func (f *factory) New(log logging.Logger) (interface{}, error) {
	config := &subprocess.Config{
		Stderr:           log,
		Stdout:           log,
		HandshakeTimeout: f.handshakeTimeout,
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

	clientConn, err := grpcutils.Dial(status.Addr)
	if err != nil {
		return nil, err
	}

	vm := NewClient(clientConn)
	vm.SetProcess(stopper, status.Pid, f.processTracker)

	f.runtimeTracker.TrackRuntime(stopper)

	return vm, nil
}
