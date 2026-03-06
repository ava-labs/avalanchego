// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaturedAverager(t *testing.T) {
	halflife := 5 * time.Minute
	startTime := time.Time{}

	matured := NewMaturedAverager(halflife, NewAverager(0, halflife, startTime))

	// Before any observations, should return zero
	require.Zero(t, matured.Read())

	// Observe a value
	matured.Observe(100.0, startTime)

	// Immediately after first observation (before halflife), should return zero
	require.Zero(t, matured.Read())

	// Observe again before halflife has passed
	matured.Observe(200.0, startTime.Add(2*time.Minute))
	require.Zero(t, matured.Read())

	// After halflife has passed, should return non-zero value
	matured.Observe(300.0, startTime.Add(5*time.Minute))
	require.NotZero(t, matured.Read())
}
