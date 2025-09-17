package firewood

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func Test_Firewood_Sync(t *testing.T) {
	tests := []int{0, 1, 1_000, 10_000, 100_000, 1_000_000}
	for _, numKeys := range tests {
		t.Run(fmt.Sprintf("numKeys=%d", numKeys), func(t *testing.T) {
			require := require.New(t)
			now := time.Now()
			fullDB := generateDB(t, numKeys, now.UnixNano())
			db := generateDB(t, 0, 0) // empty DB

			root, err := fullDB.GetMerkleRoot(context.Background())
			require.NoError(err)

			ctx := context.Background()
			syncer, err := xsync.NewManager(
				db,
				xsync.ManagerConfig{
					RangeProofClient:      p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetRangeProofHandler(fullDB)),
					ChangeProofClient:     p2ptest.NewSelfClient(t, ctx, ids.EmptyNodeID, xsync.NewGetChangeProofHandler(fullDB)),
					SimultaneousWorkLimit: 5,
					Log:                   logging.NoLog{},
					TargetRoot:            root,
				},
				prometheus.NewRegistry(),
			)
			require.NoError(err)
			require.NotNil(syncer)

			// Add logging for actual time syncing
			now = time.Now()
			require.NoError(syncer.Start(ctx))
			require.NoError(syncer.Wait(ctx))
			t.Logf("synced %d keys in %s", numKeys, time.Since(now))
		})
	}
}

func generateDB(t *testing.T, numKeys int, seed int64) *DB {
	t.Helper()
	folder := t.TempDir()
	path := folder + "/firewood.db"

	fw, err := ffi.New(path, ffi.DefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, fw)
	t.Cleanup(func() {
		require.NoError(t, fw.Close())
	})

	db := New(fw)
	if numKeys == 0 {
		return db
	}

	var (
		r         = rand.New(rand.NewSource(seed)) // #nosec G404
		keys      = make([][]byte, numKeys)
		vals      = make([][]byte, numKeys)
		minLength = 0
		maxLength = 64
	)
	t.Logf("generating %d random keys/values with seed %d", numKeys, seed)
	for i := 0; i < numKeys; i++ {
		// Random length between minLength and maxLength inclusive
		keyLen := r.Intn(maxLength-minLength+1) + minLength
		valLen := r.Intn(maxLength-minLength+1) + minLength

		key := make([]byte, keyLen)
		val := make([]byte, valLen)

		// Fill with random bytes
		_, err := r.Read(key)
		require.NoError(t, err, "read never errors")
		_, err = r.Read(val)
		require.NoError(t, err, "read never errors")

		keys[i] = key
		vals[i] = val
	}

	_, err = fw.Update(keys, vals)
	require.NoError(t, err)

	return db
}
