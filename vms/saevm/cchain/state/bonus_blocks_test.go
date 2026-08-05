// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/coreth/ethclient"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

var (
	//go:embed bonus_block_consumers.json
	bonusBlockConsumersJSON []byte

	// bonusBlockConsumers is the set of mainnet block heights which consumed
	// UTXOs left unconsumed by [mainnetBonusBlocks].
	bonusBlockConsumers set.Set[uint64]
)

func init() {
	if err := json.Unmarshal(bonusBlockConsumersJSON, &bonusBlockConsumers); err != nil {
		panic(err)
	}
}

// TestBonusBlocks asserts two properties of the mainnet bonus blocks and the
// blocks in [bonusBlockConsumers].
//
// First, every UTXO referenced by a bonus block is consumed by a non-bonus
// block. A bonus block credits the EVM without applying its shared memory
// operations, so the UTXOs its transaction references are left spendable. Each
// is therefore consumed by some other block, either an earlier block containing
// the same transaction or an unrelated later import.
//
// It's important that all bonus block UTXOs are consumed, as otherwise nodes
// may have diverged views on the available UTXO set.
//
// Second, no block after a bonus block includes that bonus block's transaction.
// Bonus blocks leave an existing index entry untouched, so the height reported
// for their transaction is the first one to accept it. A later inclusion by a
// non-bonus block would overwrite that entry and move the reported height
// forward.
func TestBonusBlocks(t *testing.T) {
	const (
		url = primary.MainnetAPIURI + "/ext/bc/C/rpc"
		// envVar must be set to run the test.
		envVar = "SAEVM_TEST_MAINNET_API"
	)
	if os.Getenv(envVar) == "" {
		t.Skipf("set %s to run: this test queries %s", envVar, url)
	}

	require.False(t, bonusBlockConsumers.Overlaps(bonusBlocks), "consumers overlap the bonus blocks")

	client, err := ethclient.DialContext(t.Context(), url)
	require.NoErrorf(t, err, "ethclient.DialContext(ctx, %q)", url)
	defer client.Close()

	bonusTxs := atomicTxs(t, client, bonusBlocks)
	consumerTxs := atomicTxs(t, client, bonusBlockConsumers)

	bonusUTXOs := consumedUTXOs(bonusTxs)
	bonusUTXOs.Difference(consumedUTXOs(consumerTxs))
	require.Emptyf(t, bonusUTXOs, "all bonus block UTXOs must be consumed")

	for consumerHeight, consumerTx := range consumerTxs {
		for bonusHeight, bonusTx := range bonusTxs {
			if consumerTx.ID() == bonusTx.ID() {
				require.Lessf(t, consumerHeight, bonusHeight, "%s included after bonus block %d", consumerTx.ID(), bonusHeight)
			}
		}
	}
}

// atomicTxs returns the cross-chain transaction in each of the blocks at
// heights.
func atomicTxs(tb testing.TB, client *ethclient.Client, heights set.Set[uint64]) map[uint64]*tx.Tx {
	tb.Helper()

	var (
		ctx = tb.Context()
		txs = make(map[uint64]*tx.Tx, heights.Len())
	)
	for height := range heights {
		block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(height))
		require.NoErrorf(tb, err, "%T.BlockByNumber(ctx, %d)", client, height)

		t, err := tx.Parse(customtypes.BlockExtData(block))
		require.NoErrorf(tb, err, "tx.Parse(customtypes.BlockExtData(%d))", height)
		txs[height] = t
	}
	return txs
}

// consumedUTXOs returns the UTXO IDs consumed by txs.
func consumedUTXOs(txs map[uint64]*tx.Tx) set.Set[ids.ID] {
	var consumed set.Set[ids.ID]
	for _, t := range txs {
		consumed.Union(t.InputIDs())
	}
	return consumed
}
