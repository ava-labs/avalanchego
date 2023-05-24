// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAverager(t *testing.T) {
	halflife := time.Second
	currentTime := time.Now()

	a := NewSyncAverager(NewAverager(0, halflife, currentTime))
	expectedValue := float64(0)
	require.Equal(t, expectedValue, a.Read())

	currentTime = currentTime.Add(halflife)
	a.Observe(1, currentTime)
	expectedValue = 1.0 / 1.5
	require.Equal(t, expectedValue, a.Read())
}

func TestAveragerTimeTravel(t *testing.T) {
	halflife := time.Second
	currentTime := time.Now()

	a := NewSyncAverager(NewAverager(1, halflife, currentTime))
	expectedValue := float64(1)
	require.Equal(t, expectedValue, a.Read())

	currentTime = currentTime.Add(-halflife)
	a.Observe(0, currentTime)
	expectedValue = 1.0 / 1.5
	require.Equal(t, expectedValue, a.Read())
}

func TestUninitializedAverager(t *testing.T) {
	halfLife := time.Second
	currentTime := time.Now()

	firstObservation := float64(10)

	a := NewUninitializedAverager(halfLife)
	require.Equal(t, 0.0, a.Read())

	a.Observe(firstObservation, currentTime)
	require.Equal(t, firstObservation, a.Read())
}
