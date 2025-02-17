// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

func newDBValue(value []byte) []byte {
	dbValue := make([]byte, len(value)+1)
	copy(dbValue[1:], value)
	return dbValue
}

func parseDBValue(dbValue []byte) ([]byte, bool) {
	if len(dbValue) == 0 {
		return nil, false
	}
	return dbValue[1:], true
}
