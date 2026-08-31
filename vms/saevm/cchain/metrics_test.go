// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
)

func TestMinBlockDelayMetric(t *testing.T) {
	sk := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(sk.EthAddress()))
	w := newWallet(sk, sut.ctx, sut.Client)

	require.Zerof(t, testutil.ToFloat64(sut.metrics.minBlockDelay), "min block delay before executing any block")

	blk := sut.issueAndExecute(ctx, t, w.newMinimalTx(t))

	want := delayExponent(blk.Header()).DelayDuration().Seconds()
	require.Equalf(t, want, testutil.ToFloat64(sut.metrics.minBlockDelay), "min block delay after executing block %d", blk.NumberU64())
}
