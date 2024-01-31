// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowRoll(t *testing.T) {
	require := require.New(t)

	var win Window
	for i := 0; i < WindowSize; i++ {
		win[i] = uint64(i + 2024)
	}

	for i := 0; i < WindowSize; i++ {
		rolledWin := Roll(win, i)

		// check that first i elements in window are shited out and
		// ovewritted by remaining WindowSize - i elements
		require.Equal(rolledWin[0:WindowSize-i], win[i:WindowSize])

		// check that trailing i elemnts of the rolled window are zero
		require.Equal(rolledWin[WindowSize-i:], make([]uint64, i))
	}

	// check that overolling wipes all window out
	overRolledWin := Roll(win, WindowSize+1)
	require.Equal(overRolledWin, Window{})
}

func TestSum(t *testing.T) {
	require := require.New(t)

	// no overflow case
	var win Window
	for i := 0; i < WindowSize; i++ {
		win[i] = uint64(i + 1)
	}
	require.Equal(Sum(win), uint64(WindowSize*(WindowSize+1)/2))

	// overflow case
	Update(&win, 0, math.MaxUint64-1)
	require.Equal(Sum(win), uint64(math.MaxUint64))

	// another overflow case
	Update(&win, 0, math.MaxUint64)
	require.Equal(Sum(win), uint64(math.MaxUint64))
}
