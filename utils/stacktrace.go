// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "runtime"

func GetStacktrace(all bool) string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, all)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, len(buf)*2)
	}
}
