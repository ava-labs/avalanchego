package firewood

import (
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/stretchr/testify/require"
)

func generateDB(t *testing.T, numKeys int) *DB {
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
		now       = time.Now().UnixNano()
		r         = rand.New(rand.NewSource(now)) // #nosec G404
		keys      = make([][]byte, numKeys)
		vals      = make([][]byte, numKeys)
		minLength = 0
		maxLength = 64
	)
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
