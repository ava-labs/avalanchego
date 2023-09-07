// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"os/exec"
	"sync"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
	"go.uber.org/zap"
)

func NewStopper(log logging.Logger, cmd *exec.Cmd) runtime.Stopper {
	return &stopper{
		cmd: cmd,
		log: log,
	}
}

type stopper struct {
	once sync.Once
	cmd  *exec.Cmd
	log  logging.Logger
}

func (s *stopper) Stop(ctx context.Context) {
	s.once.Do(func() {
		err := s.cmd.Process.Kill()
		if err == nil {
			s.log.Debug("subprocess was killed")
		} else {
			s.log.Error("subprocess was killed",
				zap.Error(err),
			)
		}
	})
}
