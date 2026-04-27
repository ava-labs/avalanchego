// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	} else if t.T != nil {
		require.False(t.T, t.CantRegisterTimout, "Unexpectedly called RegisterTimeout")
	}
}
