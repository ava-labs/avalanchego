// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/plantstore"
)

// TestPlantStoreSchemes runs the C-Chain VM on the plant's null and ours
// state schemes: genesis, a few settled blocks, and RPC reads at every height
// (older roots inside the stores' window). Block headers carry settled state
// roots, so with ours every header root MUST equal hashdb's.
func TestPlantStoreSchemes(t *testing.T) {
	const numBlocks = 8

	key := txtest.NewKey(t)
	sender := key.EthAddress()
	run := func(t *testing.T, scheme string) []common.Hash {
		timeOpt, clock := withVMTime(testStartTime)
		ctx, sut := newSUT(t,
			withMaxAllocFor(sender), timeOpt, withDB(memdb.New()), withChainDataDir(t.TempDir()),
			options.Func[sutConfig](func(c *sutConfig) { c.vmConfig.StateScheme = scheme }),
		)
		w := newWallet(key, sut.ctx, sut.Client)
		for range numBlocks {
			blk := sut.issueAndExecute(ctx, t, w.newMinimalTx(t))
			clock.AdvanceToSettle(ctx, t, blk)
		}

		var roots []common.Hash
		for h := range uint64(numBlocks + 1) {
			got, err := sut.ethclient.NonceAt(ctx, sender, new(big.Int).SetUint64(h))
			require.NoErrorf(t, err, "%T.NonceAt(%d)", sut.ethclient, h)
			assert.Equalf(t, h, got, "%T.NonceAt(%d): one export per block", sut.ethclient, h)

			hdr, err := sut.ethclient.HeaderByNumber(ctx, new(big.Int).SetUint64(h))
			require.NoErrorf(t, err, "%T.HeaderByNumber(%d)", sut.ethclient, h)
			roots = append(roots, hdr.Root)
		}
		return roots
	}

	hash := run(t, rawdb.HashScheme)
	for _, scheme := range []string{plantstore.NullScheme, plantstore.OursScheme} {
		t.Run(scheme, func(t *testing.T) {
			got := run(t, scheme)
			require.Equal(t, hash[0], got[0], "genesis root")
			if scheme == plantstore.OursScheme {
				require.Equal(t, hash, got, "header roots")
			} else {
				require.NotEqual(t, hash[numBlocks], got[numBlocks], "null roots are fake")
			}
		})
	}
}
