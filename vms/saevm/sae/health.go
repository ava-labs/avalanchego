// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
)

// HealthCheck returns the current health status of the VM.
func (vm *VM) HealthCheck(context.Context) (any, error) {
	return nil, vm.exec.HealthCheck()
}
