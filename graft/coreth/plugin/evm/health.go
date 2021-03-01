// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

// Health returns nil if this chain is healthy.
// Also returns details, which should be one of:
// string, []byte, map[string]string
func (vm *VM) HealthCheck() (interface{}, error) {
	// TODO perform actual health check
	return nil, nil
}
