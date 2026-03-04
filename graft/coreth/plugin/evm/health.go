// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "context"

// Health returns nil if this chain is healthy.
// Also returns details, which should be one of:
// string, []byte, map[string]string
func (*VM) HealthCheck(context.Context) (interface{}, error) {
	// TODO perform actual health check
	return nil, nil
}
