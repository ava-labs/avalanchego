// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
)

// Verifies that [hook.SettledGasTime] reports [hook.ErrNoSettlementMarker]
// when the settler header carries no effective settlement marker — absent or
// self-settling — instead of silently treating it as settling nothing.
func TestSettledGasTimeNoSettlementMarker(t *testing.T) {
	stub := hookstest.NewStub(1e6)
	settledHdr := &types.Header{Number: big.NewInt(1)}

	t.Run("markerless_settler", func(t *testing.T) {
		settlerHdr := &types.Header{Number: big.NewInt(2)}
		_, err := hook.SettledGasTime(stub, settledHdr, settlerHdr)
		require.ErrorIs(t, err, hook.ErrNoSettlementMarker, "hook.SettledGasTime() for a settler carrying no settlement marker")
	})

	t.Run("self_settling_settler", func(t *testing.T) {
		settlerHdr := &types.Header{Number: big.NewInt(2)}
		block, err := hookstest.BuildBlock(settlerHdr, nil, nil, nil, nil, hook.Settled{Height: 2})
		require.NoError(t, err, "hookstest.BuildBlock()")
		_, err = hook.SettledGasTime(stub, settledHdr, block.Header())
		require.ErrorIs(t, err, hook.ErrNoSettlementMarker, "hook.SettledGasTime() for a self-settling settler")
	})
}

func TestIsSynchronous(t *testing.T) {
	stub := hookstest.NewStub(1e6)

	// A marker settling a strict ancestor is asynchronous (built under SAE); a
	// self-settling marker (Height == own block number) is impossible for SAE
	// and so counts as no marker — synchronous, like a markerless pre-SAE
	// header or the Helicon genesis.
	for name, tt := range map[string]struct {
		settled       *hook.Settled // nil for a header carrying no marker
		headerNumber  int64
		isSynchronous bool
	}{
		"markerless_header": {
			settled:       nil,
			headerNumber:  1,
			isSynchronous: true,
		},
		"zero_marker": {
			settled:       &hook.Settled{},
			headerNumber:  1,
			isSynchronous: false,
		},
		"self_referential_marker": {
			settled:       &hook.Settled{Height: 1},
			headerNumber:  1,
			isSynchronous: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			hdr := &types.Header{Number: big.NewInt(tt.headerNumber)}
			if tt.settled != nil {
				block, err := hookstest.BuildBlock(hdr, nil, nil, nil, nil, *tt.settled)
				require.NoError(t, err, "hookstest.BuildBlock()")
				hdr = block.Header()
			}
			require.Equal(t, tt.isSynchronous, hook.IsSynchronous(stub, hdr), "IsSynchronous()")
		})
	}
}
