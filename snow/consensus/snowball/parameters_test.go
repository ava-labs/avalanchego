// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParametersVerify(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(p.Verify())
}

func TestParametersAnotherVerify(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          28,
		BetaRogue:             30,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(p.Verify())
}

func TestParametersYetAnotherVerify(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          3,
		BetaRogue:             3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.NoError(p.Verify())
}

func TestParametersInvalidK(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     0,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersInvalidAlpha(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 0,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersInvalidBetaVirtuous(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          0,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersInvalidBetaRogue(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             0,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersAnotherInvalidBetaRogue(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          28,
		BetaRogue:             3,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersInvalidConcurrentRepolls(t *testing.T) {
	tests := []Parameters{
		{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     2,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     0,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
	}
	for _, p := range tests {
		label := fmt.Sprintf("ConcurrentRepolls=%d", p.ConcurrentRepolls)
		t.Run(label, func(t *testing.T) {
			require.ErrorIs(t, p.Verify(), ErrParametersInvalid)
		})
	}
}

func TestParametersInvalidOptimalProcessing(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     0,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersInvalidMaxOutstandingItems(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   0,
		MaxItemProcessingTime: 1,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}

func TestParametersInvalidMaxItemProcessingTime(t *testing.T) {
	require := require.New(t)

	p := Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 0,
	}

	require.ErrorIs(p.Verify(), ErrParametersInvalid)
}
