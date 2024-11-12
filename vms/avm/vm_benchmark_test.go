// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func BenchmarkLoadUser(b *testing.B) {
	runLoadUserBenchmark := func(b *testing.B, numKeys int) {
		require := require.New(b)

		env := setup(b, &envConfig{
			fork: upgradetest.Latest,
			keystoreUsers: []*user{{
				username: username,
				password: password,
			}},
		})
		defer env.vm.ctx.Lock.Unlock()

		user, err := keystore.NewUserFromKeystore(env.vm.ctx.Keystore, username, password)
		require.NoError(err)

		keys, err := keystore.NewKeys(user, numKeys)
		require.NoError(err)

		b.ResetTimer()

		fromAddrs := set.Set[ids.ShortID]{}
		for n := 0; n < b.N; n++ {
			addrIndex := n % numKeys
			fromAddrs.Clear()
			fromAddrs.Add(keys[addrIndex].PublicKey().Address())
			_, _, err := env.vm.LoadUser(username, password, fromAddrs)
			require.NoError(err)
		}

		b.StopTimer()

		require.NoError(user.Close())
	}

	benchmarkSize := []int{10, 100, 1000, 5000}
	for _, numKeys := range benchmarkSize {
		b.Run(fmt.Sprintf("NumKeys=%d", numKeys), func(b *testing.B) {
			runLoadUserBenchmark(b, numKeys)
		})
	}
}

// getAllUTXOsBenchmark is a helper func to benchmark the GetAllUTXOs depending on the size
func getAllUTXOsBenchmark(b *testing.B, utxoCount int, randSrc rand.Source) {
	require := require.New(b)

	env := setup(b, &envConfig{fork: upgradetest.Latest})
	defer env.vm.ctx.Lock.Unlock()

	addr := ids.GenerateTestShortID()

	for i := 0; i < utxoCount; i++ {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: uint32(randSrc.Int63()),
			},
			Asset: avax.Asset{ID: env.vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 100000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{addr},
					Threshold: 1,
				},
			},
		}

		env.vm.state.AddUTXO(utxo)
	}
	require.NoError(env.vm.state.Commit())

	addrsSet := set.Of(addr)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Fetch all UTXOs older version
		notPaginatedUTXOs, err := avax.GetAllUTXOs(env.vm.state, addrsSet)
		require.NoError(err)
		require.Len(notPaginatedUTXOs, utxoCount)
	}
}

func BenchmarkGetUTXOs(b *testing.B) {
	tests := []struct {
		name      string
		utxoCount int
	}{
		{
			name:      "100",
			utxoCount: 100,
		},
		{
			name:      "10k",
			utxoCount: 10_000,
		},
		{
			name:      "100k",
			utxoCount: 100_000,
		},
	}

	for testIdx, count := range tests {
		randSrc := rand.NewSource(int64(testIdx))
		b.Run(count.name, func(b *testing.B) {
			getAllUTXOsBenchmark(b, count.utxoCount, randSrc)
		})
	}
}
