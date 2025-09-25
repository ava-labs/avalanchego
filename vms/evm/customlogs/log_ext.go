// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customlogs

import "github.com/ava-labs/libevm/core/types"

// FlattenLogs converts a nested array of logs to a single array of logs.
func FlattenLogs(list [][]*ethtypes.Log) []*ethtypes.Log {
	numLogs := 0
	for _, logs := range list {
		totalLen += len(logs)
	}

	// Pre-allocate slice with exact capacity
	flat := make([]*ethtypes.Log, 0, totalLen)
	for _, logs := range list {
		flat = append(flat, logs...)
	}
	return flat
}
