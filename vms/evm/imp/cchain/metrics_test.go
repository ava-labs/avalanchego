// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/tx/txtest"
)

func TestMinBlockDelayMetric(t *testing.T) {
	sk := txtest.NewKey(t)
	ctx, sut := newSUT(t, withMaxAllocFor(sk.EthAddress()))
	w := newWallet(sk, sut.ctx, sut.Client)

	require.Zerof(t, gaugeValue(t, sut.ctx.Metrics, "cchain_min_block_delay_seconds"), "min block delay before executing any block")

	blk := sut.issueAndExecute(ctx, t, w.newMinimalTx(t))

	want := delayExponent(blk.Header()).DelayDuration().Seconds()
	require.Equalf(t, want, gaugeValue(t, sut.ctx.Metrics, "cchain_min_block_delay_seconds"), "min block delay after executing block %d", blk.NumberU64())
}

// gaugeValue returns the value of the unlabeled gauge named name in g.
func gaugeValue(t *testing.T, g prometheus.Gatherer, name string) float64 {
	t.Helper()

	families, err := g.Gather()
	require.NoError(t, err, "Gather()")
	for _, f := range families {
		if f.GetName() == name {
			require.Lenf(t, f.GetMetric(), 1, "%q metrics", name)
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	require.FailNowf(t, "metric not found", "%q", name)
	return 0
}
