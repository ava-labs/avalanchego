// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"fmt"
)

// HealthCheck returns the current health status of the VM. It reports
// unhealthy if [saexec.Executor] has permanently stopped executing blocks, so
// that the failure is detected by health-based monitoring (e.g. liveness
// probes) even if no new block is accepted for a while to otherwise surface it
// via [VM.Accept].
func (vm *VM) HealthCheck(context.Context) (any, error) {
	if err := vm.exec.TerminalError(); err != nil {
		return nil, fmt.Errorf("asynchronous execution permanently stopped: %w", err)
	}
	return nil, nil
}
