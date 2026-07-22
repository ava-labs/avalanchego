// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

// Storage kinds for the KV-database axis of the run configuration.
const (
	kvMemDB   = "memdb"
	kvLevelDB = "leveldb"
)

// runConfig is the once-per-check configuration of a model run: accounts and
// their genesis balances, dynamic-parameter votes, and the storage layer.
type runConfig struct {
	numAccounts uint
	balanceExps []int // genesis balance of account i is 10^balanceExps[i] wei

	gasTarget   *gas.Gas
	priceTarget *gas.Price
	minDelayMS  *uint64

	kv             string // kvMemDB or kvLevelDB
	scheme         string // rawdb.HashScheme or customrawdb.FirewoodScheme
	commitInterval uint64

	numValidators int
	numAtomicKeys int
}

func genRunConfig() *rapid.Generator[runConfig] {
	return rapid.Custom(func(rt *rapid.T) runConfig {
		c := runConfig{
			numAccounts: uint(rapid.IntRange(2, 6).Draw(rt, "numAccounts")), //#nosec G115 -- bounded draw, 2..6
			// Weighted draws: repeats in the sample set set the odds. Weighted
			// toward memdb/HashScheme (the fast paths) to keep the PR-mode CI
			// budget in check while still reaching leveldb/Firewood regularly.
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
			numValidators:  rapid.IntRange(1, 3).Draw(rt, "numValidators"),
			numAtomicKeys:  rapid.IntRange(1, 2).Draw(rt, "numAtomicKeys"),
		}
		// Log-scale balances: from "barely funds a few txs" to effectively
		// unbounded, so funding-edge behaviour is reachable.
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
		return c
	})
}

// storageOptions returns the sutOption applying c's storage axes to the VM
// config. Firewood always persists under the chain data dir and needs a
// non-zero trie cache; both are handled by saedb defaults.
func (c runConfig) storageOptions() sutOption {
	return options.Func[sutConfig](func(sc *sutConfig) {
		sc.vmConfig.StateScheme = c.scheme
		sc.vmConfig.CommitInterval = c.commitInterval
	})
}

// balance returns account i's genesis balance, 10^balanceExps[i] wei.
func (c runConfig) balance(i int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(c.balanceExps[i])), nil)
}

func TestGenRunConfig(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genRunConfig().Draw(rt, "runConfig")
		require.GreaterOrEqual(rt, cfg.numAccounts, uint(2), "numAccounts lower bound")
		require.LessOrEqual(rt, cfg.numAccounts, uint(6), "numAccounts upper bound")
		numAccounts := int(cfg.numAccounts) //#nosec G115 -- bounded draw, 2..6
		require.Len(rt, cfg.balanceExps, numAccounts, "one balance exponent per account")
		require.Contains(rt, []string{kvMemDB, kvLevelDB}, cfg.kv, "kv store kind")
		require.Contains(rt, []string{rawdb.HashScheme, customrawdb.FirewoodScheme}, cfg.scheme, "trie scheme")
		require.NotZero(rt, cfg.commitInterval, "commit interval")
	})
}
