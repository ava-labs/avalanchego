// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShutdownBeforeInitialize(t *testing.T) {
	o := New(nil)
	require.NoError(t, o.Shutdown(t.Context()))
}
