// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

func TestFlat(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K: 2, Alpha: 2, BetaVirtuous: 1, BetaRogue: 2,
	}
	f := Flat{}
	f.Initialize(params, Red)
	f.Add(Green)
	f.Add(Blue)

	require.Equal(Red, f.Preference())
	require.False(f.Finalized())

	twoBlue := bag.Bag[ids.ID]{}
	twoBlue.Add(Blue, Blue)
	require.True(f.RecordPoll(twoBlue))
	require.Equal(Blue, f.Preference())
	require.False(f.Finalized())

	oneRedOneBlue := bag.Bag[ids.ID]{}
	oneRedOneBlue.Add(Red, Blue)
	require.False(f.RecordPoll(oneRedOneBlue))
	require.Equal(Blue, f.Preference())
	require.False(f.Finalized())

	require.True(f.RecordPoll(twoBlue))
	require.Equal(Blue, f.Preference())
	require.False(f.Finalized())

	require.True(f.RecordPoll(twoBlue))
	require.Equal(Blue, f.Preference())
	require.True(f.Finalized())

	expected := "SB(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES, NumSuccessfulPolls = 3, SF(Confidence = 2, Finalized = true, SL(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES)))"
	require.Equal(expected, f.String())
}
