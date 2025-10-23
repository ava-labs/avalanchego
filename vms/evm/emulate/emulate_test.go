// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package emulate

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	cchain "github.com/ava-labs/coreth/plugin/evm/customtypes"
	subnet "github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
)

// setAndGetMillis is an arbitrary function that can be run if and only if
// emulating either `coreth` or `subnet-evm`. If the respective emulation isn't
// active then it will cause `libevm` to panic. In addition to the panicking
// behaviour, it asserts that it is the only active emulation.
func setAndGetMillis[T interface {
	any
	*cchain.HeaderExtra | *subnet.HeaderExtra
}](
	t *testing.T,
	active *atomic.Int64,
	withExtra func(*types.Header, T) *types.Header,
	extra T,
	blockMillis func(*types.Block) *uint64,
	retErr error,
) func() (*uint64, error) {
	return func() (*uint64, error) {
		require.True(t, active.CompareAndSwap(0, 1))
		defer func() {
			require.True(t, active.CompareAndSwap(1, 0))
		}()

		b := types.NewBlockWithHeader(withExtra(
			&types.Header{},
			extra,
		))
		return blockMillis(b), retErr
	}
}

func TestEmulation(t *testing.T) {
	const milli = uint64(1234)
	newUint64 := func(u uint64) *uint64 { return &u }
	sentinel := errors.New("uh oh")

	var active atomic.Int64
	onCChain := setAndGetMillis(
		t, &active,
		cchain.WithHeaderExtra,
		&cchain.HeaderExtra{TimeMilliseconds: newUint64(milli)},
		cchain.BlockTimeMilliseconds,
		sentinel,
	)
	onSubnetEVM := setAndGetMillis(
		t, &active,
		subnet.WithHeaderExtra,
		&subnet.HeaderExtra{TimeMilliseconds: newUint64(milli)},
		subnet.BlockTimeMilliseconds,
		sentinel,
	)

	start := make(chan struct{})

	t.Run("coreth", func(t *testing.T) {
		t.Parallel()
		<-start
		for range 1000 {
			got, err := CChainVal(onCChain)
			require.ErrorIs(t, err, sentinel)
			require.Equal(t, milli, *got)
		}
	})

	t.Run("subnet-evm", func(t *testing.T) {
		t.Parallel()
		<-start
		for range 1000 {
			got, err := SubnetEVMVal(onSubnetEVM)
			require.ErrorIs(t, err, sentinel)
			require.Equal(t, milli, *got)
		}
	})

	close(start)
}
