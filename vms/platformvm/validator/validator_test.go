// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
)

const defaultWeight = 10000

// each key controls an address that has [defaultBalance] AVAX at genesis
var keys = crypto.BuildTestKeys()

func TestValidatorBoundedBy(t *testing.T) {
	require := require.New(t)

	// case 1: a starts, a finishes, b starts, b finishes
	aStartTime := uint64(0)
	aEndTIme := uint64(1)
	a := &Validator{
		NodeID: ids.NodeID(keys[0].PublicKey().Address()),
		Start:  aStartTime,
		End:    aEndTIme,
		Wght:   defaultWeight,
	}

	bStartTime := uint64(2)
	bEndTime := uint64(3)
	b := &Validator{
		NodeID: ids.NodeID(keys[0].PublicKey().Address()),
		Start:  bStartTime,
		End:    bEndTime,
		Wght:   defaultWeight,
	}
	require.False(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.False(b.BoundedBy(a.StartTime(), a.EndTime()))

	// case 2: a starts, b starts, a finishes, b finishes
	a.Start = 0
	b.Start = 1
	a.End = 2
	b.End = 3
	require.False(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.False(b.BoundedBy(a.StartTime(), a.EndTime()))

	// case 3: a starts, b starts, b finishes, a finishes
	a.Start = 0
	b.Start = 1
	b.End = 2
	a.End = 3
	require.False(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.True(b.BoundedBy(a.StartTime(), a.EndTime()))

	// case 4: b starts, a starts, a finishes, b finishes
	b.Start = 0
	a.Start = 1
	a.End = 2
	b.End = 3
	require.True(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.False(b.BoundedBy(a.StartTime(), a.EndTime()))

	// case 5: b starts, b finishes, a starts, a finishes
	b.Start = 0
	b.End = 1
	a.Start = 2
	a.End = 3
	require.False(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.False(b.BoundedBy(a.StartTime(), a.EndTime()))

	// case 6: b starts, a starts, b finishes, a finishes
	b.Start = 0
	a.Start = 1
	b.End = 2
	a.End = 3
	require.False(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.False(b.BoundedBy(a.StartTime(), a.EndTime()))

	// case 3: a starts, b starts, b finishes, a finishes
	a.Start = 0
	b.Start = 0
	b.End = 1
	a.End = 1
	require.True(a.BoundedBy(b.StartTime(), b.EndTime()))
	require.True(b.BoundedBy(a.StartTime(), a.EndTime()))
}
