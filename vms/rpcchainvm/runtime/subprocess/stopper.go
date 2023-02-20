// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"os/exec"
	"sync"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
)

func NewStopper(logger logging.Logger, cmd *exec.Cmd) runtime.Stopper {
	return &stopper{
		cmd:    cmd,
		logger: logger,
	}
}

type stopper struct {
	lock     sync.Mutex
	cmd      *exec.Cmd
	shutdown bool

	logger logging.Logger
}

func (s *stopper) Stop(ctx context.Context) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// subsequent calls to this method are a no-op
	if s.shutdown || s.cmd.Process == nil {
		return
	}

	s.shutdown = true
	stop(ctx, s.logger, s.cmd)
}
