package merkledb

import (
	"context"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func newTestConfig() Config {
	return Config{
		Reg:          prometheus.NewRegistry(),
		Tracer:       trace.Noop,
		BranchFactor: BranchFactor16,
	}
}

func GenerateTrie(t *testing.T, r *rand.Rand, count int) (MerkleDB, error) {
	return GenerateTrieWithMinKeyLen(t, r, count, 0)
}

func GenerateTrieWithMinKeyLen(t *testing.T, r *rand.Rand, count int, minKeyLen int) (MerkleDB, error) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		newTestConfig(),
	)
	if err != nil {
		return nil, err
	}
	var (
		allKeys  [][]byte
		seenKeys = make(map[string]struct{})
		batch    = db.NewBatch()
	)
	genKey := func() []byte {
		// new prefixed key
		if len(allKeys) > 2 && r.Intn(25) < 10 {
			prefix := allKeys[r.Intn(len(allKeys))]
			key := make([]byte, r.Intn(50)+len(prefix))
			copy(key, prefix)
			_, err := r.Read(key[len(prefix):])
			require.NoError(err)
			return key
		}

		// new key
		key := make([]byte, r.Intn(50)+minKeyLen)
		_, err = r.Read(key)
		require.NoError(err)
		return key
	}

	for i := 0; i < count; {
		value := make([]byte, r.Intn(51))
		if len(value) == 0 {
			value = nil
		} else {
			_, err = r.Read(value)
			require.NoError(err)
		}
		key := genKey()
		if _, seen := seenKeys[string(key)]; seen {
			continue // avoid duplicate keys so we always get the count
		}
		allKeys = append(allKeys, key)
		seenKeys[string(key)] = struct{}{}
		if err = batch.Put(key, value); err != nil {
			return db, err
		}
		i++
	}
	return db, batch.Write()
}
