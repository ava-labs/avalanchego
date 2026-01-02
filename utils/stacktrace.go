// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "runtime"

func GetStacktrace(all bool) string {
	buf := make([]byte, 1<<24)
	n := runtime.Stack(buf, all)
	return string(buf[:n])
}
