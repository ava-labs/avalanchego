// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Timer is a test timer
type Timer struct {
	T *testing.T

	CantRegisterTimout bool

	RegisterTimeoutF func(time.Duration)
}

func (t *Timer) RegisterTimeout(delay time.Duration) {
	if t.RegisterTimeoutF != nil {
		t.RegisterTimeoutF(delay)
	} else if t.CantRegisterTimout && t.T != nil {
		require.FailNow(t.T, "Unexpectedly called RegisterTimeout")
	}
}
