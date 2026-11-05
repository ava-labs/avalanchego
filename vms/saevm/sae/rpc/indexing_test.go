// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

type dbChain struct {
	Chain
	db ethdb.Database
}

func (c dbChain) DB() ethdb.Database {
	return c.db
}

func TestCachedBloomOverrider(t *testing.T) {
	withLogs := types.Receipts{{
		Logs: []*types.Log{{
			Address: common.Address{'a'},
			Topics:  []common.Hash{{'t'}},
		}},
	}}

	tests := []struct {
		name       string
		receipts   types.Receipts // not written if nil
		wantCached bool
	}{
		{
			name: "not_executed",
		},
		{
			name:       "executed_without_logs",
			receipts:   types.Receipts{},
			wantCached: true,
		},
		{
			name:       "executed_with_logs",
			receipts:   withLogs,
			wantCached: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			overrider := newCachedBloomOverrider(dbChain{db: db})

			header := &types.Header{Number: big.NewInt(1)}
			hash := header.Hash()
			if test.receipts != nil {
				rawdb.WriteReceipts(db, hash, 1, test.receipts)
			}

			want := types.CreateBloom(test.receipts)
			require.Equal(t, want, overrider.OverrideHeaderBloom(header))
			require.Equal(t, test.wantCached, overrider.cache.Contains(hash))

			// Whatever the database holds now can only be seen on a cache miss.
			rawdb.WriteReceipts(db, hash, 1, withLogs)
			if !test.wantCached {
				want = types.CreateBloom(withLogs)
			}
			require.Equal(t, want, overrider.OverrideHeaderBloom(header))
		})
	}
}

func BenchmarkOverrideHeaderBloom(b *testing.B) {
	kv, err := leveldb.New(b.TempDir(), nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(b, err)
	b.Cleanup(func() {
		require.NoError(b, kv.Close())
	})
	chain := dbChain{db: saetypes.NewEthDB(kv)}

	headers := make([]*types.Header, bloomCacheSize)
	for i := range headers {
		headers[i] = &types.Header{Number: big.NewInt(int64(i))}
		receipts := make(types.Receipts, 10)
		for j := range receipts {
			receipts[j] = &types.Receipt{
				Logs: []*types.Log{{
					Address: common.BigToAddress(big.NewInt(int64(i))),
					Topics:  []common.Hash{common.BigToHash(big.NewInt(int64(j)))},
				}},
			}
		}
		rawdb.WriteReceipts(chain.db, headers[i].Hash(), uint64(i), receipts)
	}
	require.NoError(b, kv.Compact(nil, nil)) // don't read from the memtable

	benchmarks := []struct {
		name      string
		overrider filters.BloomOverrider
	}{
		{
			name:      "uncached",
			overrider: bloomOverrider{chain},
		},
		{
			name:      "cached",
			overrider: newCachedBloomOverrider(chain),
		},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for _, h := range headers {
				bm.overrider.OverrideHeaderBloom(h)
			}
			var i int
			for b.Loop() {
				bm.overrider.OverrideHeaderBloom(headers[i%len(headers)])
				i++
			}
		})
	}
}
