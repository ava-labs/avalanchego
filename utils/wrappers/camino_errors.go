// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

func IgnoreError(val any, err error) interface{} { //nolint:revive
	return val
}
