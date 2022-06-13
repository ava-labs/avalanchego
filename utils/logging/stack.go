// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"runtime"
)

// TODO: remove when we transition to support structured logging

// Stacktrace can print the current stacktrace
type Stacktrace struct {
	Global bool
}

func (st Stacktrace) String() string {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, st.Global)
	return string(buf[:n])
}
