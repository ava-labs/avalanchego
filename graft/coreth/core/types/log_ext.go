// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package types

// FlattenLogs converts a nested array of logs to a single array of logs.
func FlattenLogs(list [][]*Log) []*Log {
	var flat []*Log
	for _, logs := range list {
		flat = append(flat, logs...)
	}
	return flat
}
