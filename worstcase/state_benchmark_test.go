// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worstcase

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
)

func BenchmarkApplyTxWithSnapshot(b *testing.B) {
	for _, numAccounts := range []uint{100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("accounts=%d", numAccounts), func(b *testing.B) {
			signer := types.LatestSigner(saetest.ChainConfig())
			wallet := saetest.NewUNSAFEWallet(b, numAccounts, signer)
			sut := newSUT(b, saetest.MaxAllocFor(wallet.Addresses()...))

			tx := wallet.SetNonceAndSign(b, 0, &types.LegacyTx{
				To:       &common.Address{},
				Gas:      params.TxGas,
				GasPrice: big.NewInt(1),
			})

			snaps, err := snapshot.New(
				snapshot.Config{
					AsyncBuild: false,
					CacheSize:  saedb.SnapshotCacheSizeMB,
				},
				sut.db, sut.stateCache.TrieDB(), sut.genesis.PostExecutionStateRoot(),
			)
			require.NoError(b, err, "snapshot.New()")
			b.Cleanup(snaps.Release)

			hdr := &types.Header{
				ParentHash: sut.genesis.Hash(),
				Number:     big.NewInt(1),
			}

			for _, tt := range []struct {
				name  string
				snaps *snapshot.Tree
			}{
				{name: "without_snapshot", snaps: nil},
				{name: "with_snapshot", snaps: snaps},
			} {
				b.Run(tt.name, func(b *testing.B) {
					b.StopTimer()
					for range b.N {
						opener := saetest.NewStateDBOpener(sut.stateCache, tt.snaps)
						s, err := NewState(sut.hooks, sut.config, sut.genesis, opener)
						require.NoError(b, err, "NewState()")
						require.NoError(b, s.StartBlock(hdr), "StartBlock()")

						b.StartTimer()
						err = s.ApplyTx(tx)
						b.StopTimer()
						require.NoError(b, err, "ApplyTx()")
					}
				})
			}
		})
	}
}
