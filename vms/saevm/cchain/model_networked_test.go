// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

// nodeStorage is one node's independently drawn storage configuration. Nodes
// with different storage backends must still converge on identical state — a
// free invariant of the networked model.
type nodeStorage struct {
	kv             string // kvMemDB or kvLevelDB
	scheme         string // rawdb.HashScheme or customrawdb.FirewoodScheme
	commitInterval uint64
}

// networkedRunConfig is the once-per-check configuration of a networked model
// run. The embedded runConfig contributes the shared axes (accounts, genesis
// balances, dynamic-parameter votes, numValidators); its single-node storage
// axes (kv, scheme, commitInterval) and numAtomicKeys remain zero and are
// ignored — storage is drawn per node in perNode, and this suite issues no
// cross-chain txs.
type networkedRunConfig struct {
	runConfig

	numNonValidators int
	perNode          []nodeStorage
}

func (c networkedRunConfig) numNodes() int {
	return c.numValidators + c.numNonValidators
}

func genNodeStorage() *rapid.Generator[nodeStorage] {
	return rapid.Custom(func(rt *rapid.T) nodeStorage {
		return nodeStorage{
			// Weighted draws: repeats in the sample set set the odds, matching
			// genRunConfig. memdb/HashScheme dominate to keep the CI budget in
			// check while still reaching leveldb/Firewood regularly.
			kv: rapid.SampledFrom([]string{
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvMemDB,
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvMemDB,
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvMemDB,
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvLevelDB,
			}).Draw(rt, "kv"),
			scheme: rapid.SampledFrom([]string{
				rawdb.HashScheme, rawdb.HashScheme, rawdb.HashScheme,
				rawdb.HashScheme, rawdb.HashScheme, rawdb.HashScheme,
				rawdb.HashScheme, rawdb.HashScheme, rawdb.HashScheme,
				customrawdb.FirewoodScheme,
			}).Draw(rt, "scheme"),
			commitInterval: rapid.SampledFrom([]uint64{1, 4, 16, saedb.DefaultCommitInterval}).Draw(rt, "commitInterval"),
		}
	})
}

// storageOptions returns the sutOption applying s's storage axes to one
// node's VM config.
func (s nodeStorage) storageOptions() sutOption {
	return options.Func[sutConfig](func(sc *sutConfig) {
		sc.vmConfig.StateScheme = s.scheme
		sc.vmConfig.CommitInterval = s.commitInterval
	})
}

func genNetworkedRunConfig() *rapid.Generator[networkedRunConfig] {
	return rapid.Custom(func(rt *rapid.T) networkedRunConfig {
		c := networkedRunConfig{
			runConfig: runConfig{
				numAccounts: uint(rapid.IntRange(2, 6).Draw(rt, "numAccounts")), //#nosec G115 -- bounded draw, 2..6
				// 2 validators common, 3 rare: per-action cost scales with node
				// count, and most convergence bugs need only two views.
				numValidators: rapid.SampledFrom([]int{2, 2, 2, 3}).Draw(rt, "numValidators"),
			},
			numNonValidators: rapid.IntRange(0, 1).Draw(rt, "numNonValidators"),
		}
		numAccounts := int(c.numAccounts) //#nosec G115 -- bounded draw, 2..6
		c.balanceExps = rapid.SliceOfN(rapid.IntRange(9, 30), numAccounts, numAccounts).Draw(rt, "balanceExps")
		if rapid.Bool().Draw(rt, "voteGasTarget") {
			g := gas.Gas(rapid.Uint64Range(1_000_000, 100_000_000).Draw(rt, "gasTarget"))
			c.gasTarget = &g
		}
		if rapid.Bool().Draw(rt, "votePriceTarget") {
			p := gas.Price(rapid.Uint64Range(1, 1_000_000).Draw(rt, "priceTarget"))
			c.priceTarget = &p
		}
		if rapid.Bool().Draw(rt, "voteMinDelay") {
			d := rapid.Uint64Range(1, 10_000).Draw(rt, "minDelayMS")
			c.minDelayMS = &d
		}
		n := c.numNodes()
		c.perNode = rapid.SliceOfN(genNodeStorage(), n, n).Draw(rt, "perNode")
		return c
	})
}

func TestGenNetworkedRunConfig(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genNetworkedRunConfig().Draw(rt, "networkedRunConfig")
		require.Containsf(rt, []int{2, 3}, cfg.numValidators, "numValidators")
		require.Containsf(rt, []int{0, 1}, cfg.numNonValidators, "numNonValidators")
		require.Lenf(rt, cfg.perNode, cfg.numNodes(), "one storage draw per node")
		require.GreaterOrEqualf(rt, cfg.numAccounts, uint(2), "numAccounts lower bound")
		require.LessOrEqualf(rt, cfg.numAccounts, uint(6), "numAccounts upper bound")
		numAccounts := int(cfg.numAccounts) //#nosec G115 -- bounded draw, 2..6
		require.Lenf(rt, cfg.balanceExps, numAccounts, "one balance exponent per account")
		require.Zerof(rt, cfg.numAtomicKeys, "networked suite issues no atomic txs")
		for i, s := range cfg.perNode {
			require.Containsf(rt, []string{kvMemDB, kvLevelDB}, s.kv, "node %d kv store kind", i)
			require.Containsf(rt, []string{rawdb.HashScheme, customrawdb.FirewoodScheme}, s.scheme, "node %d trie scheme", i)
			require.NotZerof(rt, s.commitInterval, "node %d commit interval", i)
		}
	})
}
