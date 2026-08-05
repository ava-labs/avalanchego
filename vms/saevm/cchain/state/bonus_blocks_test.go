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

// TestBonusBlockUTXOsConsumed asserts that every UTXO referenced by a bonus
// block is consumed by a non-bonus block in [bonusBlockConsumers].
//
// A bonus block credits the EVM without applying its shared memory operations,
// so the UTXOs its transaction references are left spendable. Each is therefore
// consumed by some other block, either an earlier block containing the same
// transaction or an unrelated later import.
//
// It's important that all bonus block UTXOs are consumed, as otherwise nodes
// may have diverged views on the available UTXO set.
func TestBonusBlockUTXOsConsumed(t *testing.T) {
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

	bonusUTXOs := consumedUTXOs(t, client, bonusBlocks)
	consumedUTXOs := consumedUTXOs(t, client, bonusBlockConsumers)

	bonusUTXOs.Difference(consumedUTXOs)
	require.Emptyf(t, bonusUTXOs, "all bonus block UTXOs must be consumed")
}

// consumedUTXOs returns the UTXO IDs consumed by the cross-chain transactions
// in the blocks at heights.
func consumedUTXOs(tb testing.TB, client *ethclient.Client, heights set.Set[uint64]) set.Set[ids.ID] {
	tb.Helper()

	var (
		ctx      = tb.Context()
		consumed set.Set[ids.ID]
	)
	for height := range heights {
		block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(height))
		require.NoErrorf(tb, err, "%T.BlockByNumber(ctx, %d)", client, height)

		t, err := tx.Parse(customtypes.BlockExtData(block))
		require.NoErrorf(tb, err, "tx.Parse(customtypes.BlockExtData(%d))", height)
		consumed.Union(t.InputIDs())
	}
	return consumed
}
