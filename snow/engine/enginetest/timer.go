// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ common.Timer = (*Timer)(nil)

// Timer is a test timer
type Timer struct {
	T *testing.T

	CantRegisterTimout bool

	RegisterTimeoutF func(time.Duration)
}

// Default set the default callable value to [cant]
func (t *Timer) Default(cant bool) {
	t.CantRegisterTimout = cant
}

func (t *Timer) RegisterTimeout(delay time.Duration) {
	if t.RegisterTimeoutF != nil {
		t.RegisterTimeoutF(delay)
	} else if t.CantRegisterTimout && t.T != nil {
		require.FailNow(t.T, "Unexpectedly called RegisterTimeout")
	}
}
