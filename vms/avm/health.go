// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

// Health implements the common.VM interface
// TODO add health checks
func (vm *VM) HealthCheck() (interface{}, error) {
	return nil, nil
}
