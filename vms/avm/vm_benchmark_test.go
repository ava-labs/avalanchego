// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func BenchmarkLoadUser(b *testing.B) {
	runLoadUserBenchmark := func(b *testing.B, numKeys int) {
		// This will segfault instead of failing gracefully if there's an error
		_, _, vm, _ := GenesisVM(nil)
		ctx := vm.ctx
		defer func() {
			if err := vm.Shutdown(context.Background()); err != nil {
				b.Fatal(err)
			}
			ctx.Lock.Unlock()
		}()

		user, err := keystore.NewUserFromKeystore(vm.ctx.Keystore, username, password)
		if err != nil {
			b.Fatalf("Failed to get user keystore db: %s", err)
		}

		keys, err := keystore.NewKeys(user, numKeys)
		if err != nil {
			b.Fatalf("problem generating private key: %s", err)
		}

		b.ResetTimer()

		fromAddrs := set.Set[ids.ShortID]{}
		for n := 0; n < b.N; n++ {
			addrIndex := n % numKeys
			fromAddrs.Clear()
			fromAddrs.Add(keys[addrIndex].PublicKey().Address())
			if _, _, err := vm.LoadUser(username, password, fromAddrs); err != nil {
				b.Fatalf("Failed to load user: %s", err)
			}
		}

		b.StopTimer()

		if err := user.Close(); err != nil {
			b.Fatal(err)
		}
	}

	benchmarkSize := []int{10, 100, 1000, 10000}
	for _, numKeys := range benchmarkSize {
		b.Run(fmt.Sprintf("NumKeys=%d", numKeys), func(b *testing.B) {
			runLoadUserBenchmark(b, numKeys)
		})
	}
}

// GetAllUTXOsBenchmark is a helper func to benchmark the GetAllUTXOs depending on the size
func GetAllUTXOsBenchmark(b *testing.B, utxoCount int) {
	_, _, vm, _ := GenesisVM(b)
	ctx := vm.ctx
	defer func() {
		if err := vm.Shutdown(context.Background()); err != nil {
			b.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	addr := ids.GenerateTestShortID()

	// #nosec G404
	for i := 0; i < utxoCount; i++ {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: rand.Uint32(),
			},
			Asset: avax.Asset{ID: ids.ID{'y', 'e', 'e', 't'}},
			Out: &secp256k1fx.TransferOutput{
				Amt: 100000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{addr},
					Threshold: 1,
				},
			},
		}

		vm.state.AddUTXO(utxo)
	}
	require.NoError(b, vm.state.Commit())

	addrsSet := set.Set[ids.ShortID]{}
	addrsSet.Add(addr)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Fetch all UTXOs older version
		notPaginatedUTXOs, err := avax.GetAllUTXOs(vm.state, addrsSet)
		require.NoError(b, err)
		require.Len(b, notPaginatedUTXOs, utxoCount)
	}
}

func BenchmarkGetUTXOs(b *testing.B) {
	tests := []struct {
		name      string
		utxoCount int
	}{
		{"100", 100},
		{"10k", 10000},
		{"100k", 100000},
	}

	for _, count := range tests {
		b.Run(count.name, func(b *testing.B) {
			GetAllUTXOsBenchmark(b, count.utxoCount)
		})
	}
}
