// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAverager(t *testing.T) {
	require := require.New(t)

	halflife := time.Second
	currentTime := time.Now()

	a := NewSyncAverager(NewAverager(0, halflife, currentTime))
	require.Zero(a.Read())

	currentTime = currentTime.Add(halflife)
	a.Observe(1, currentTime)
	require.InDelta(1.0/1.5, a.Read(), 0)
}

func TestAveragerTimeTravel(t *testing.T) {
	require := require.New(t)

	halflife := time.Second
	currentTime := time.Now()

	a := NewSyncAverager(NewAverager(1, halflife, currentTime))
	require.InDelta(float64(1), a.Read(), 0)

	currentTime = currentTime.Add(-halflife)
	a.Observe(0, currentTime)
	require.InDelta(1.0/1.5, a.Read(), 0)
}

func TestUninitializedAverager(t *testing.T) {
	require := require.New(t)

	halfLife := time.Second
	currentTime := time.Now()

	firstObservation := float64(10)

	a := NewUninitializedAverager(halfLife)
	require.Zero(a.Read())

	a.Observe(firstObservation, currentTime)
	require.InDelta(firstObservation, a.Read(), 0)
}
