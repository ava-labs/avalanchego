// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"os/exec"
	"sync"

	"go.uber.org/zap"

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
	once   sync.Once
	cmd    *exec.Cmd
	logger logging.Logger
}

// TODO: Do we still want to provide the context to Stop?
func (s *stopper) Stop(context.Context) {
	s.once.Do(func() {
		if err := s.cmd.Process.Kill(); err != nil {
			s.logger.Error("subprocess was killed",
				zap.Int("pid", s.cmd.Process.Pid),
				zap.Error(err),
			)
			return
		}
		s.logger.Debug("subprocess was killed",
			zap.Int("pid", s.cmd.Process.Pid),
		)

		// Because we killed the process, wait is expected to return a non-nil
		// error.
		err := s.cmd.Wait()
		s.logger.Debug("subprocess exited",
			zap.Int("pid", s.cmd.Process.Pid),
			zap.Error(err),
		)
	})
}
