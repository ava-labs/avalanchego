// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPivotPolicy(t *testing.T) {
	// Zero interval uses default.
	require.Equal(t, defaultPivotInterval, newPivotPolicy(0).interval)

	// Test throttling behavior.
	p := newPivotPolicy(100)

	// First call at 150 initializes threshold to ceil(150/100)*100 = 200
	require.False(t, p.shouldForward(150)) // 150 < 200
	require.False(t, p.shouldForward(199)) // 199 < 200
	require.True(t, p.shouldForward(200))  // 200 >= 200
	p.advance()                            // threshold becomes 300

	require.False(t, p.shouldForward(250)) // 250 < 300
	require.True(t, p.shouldForward(300))  // 300 >= 300
}
