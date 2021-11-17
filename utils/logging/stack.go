// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"bytes"
	"fmt"
	"runtime"
	"strconv"
)

// Stacktrace can print the current stacktrace
type Stacktrace struct {
	Global bool
}

func (st Stacktrace) String() string {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, st.Global)
	return string(buf[:n])
}

// RoutineID can print the current goroutine ID
type RoutineID struct{}

func (RoutineID) String() string {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return fmt.Sprintf("Goroutine: %d", n)
}
