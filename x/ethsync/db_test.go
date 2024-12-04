// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethsync

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestUpdateKVsWithPrefix(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	disk := memdb.New()
	db, err := New(ctx, disk, merkledb.Config{})
	require.NoError(err)
	_ = db

	ops := []merkledb.KeyValue{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("c"), Value: []byte("3")},
		{Key: []byte("d"), Value: []byte("4")},
	}
	err = db.updateKVs(nil, ops)
	require.NoError(err)

	ops2 := []merkledb.KeyValue{
		{Key: []byte("A"), Value: []byte("1")},
		{Key: []byte("B"), Value: []byte("2")},
		{Key: []byte("C"), Value: []byte("3")},
	}
	accHash := common.Hash{0x01}
	err = db.updateKVs(accHash.Bytes(), ops2)
	require.NoError(err)

	ops3 := []merkledb.KeyValue{
		{Key: []byte("e"), Value: []byte("5")},
		{Key: []byte("f"), Value: []byte("6")},
	}
	err = db.updateKVs(accHash.Bytes(), ops3)
	require.NoError(err)

	ops4 := []merkledb.KeyValue{
		{Key: []byte("g"), Value: []byte("7")},
		{Key: []byte("h"), Value: []byte("8")},
	}
	err = db.updateKVs(nil, ops4)
	require.NoError(err)

	kvs0 := make(map[string][]byte)
	require.NoError(db.getKVs(nil, nil, nil, kvs0))
	for k, v := range kvs0 {
		t.Logf("k: %s, v: %s", k, v)
	}

	t.Log("=====================================")
	kvs1 := make(map[string][]byte)
	require.NoError(db.getKVs(accHash.Bytes(), nil, nil, kvs1))
	for k, v := range kvs1 {
		t.Logf("k: %s, v: %s", k, v)
	}
}
