// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
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

func (s *stopper) Stop(_ context.Context) {
	s.once.Do(func() {
		<-time.After(runtime.DefaultGracefulTimeout)
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
