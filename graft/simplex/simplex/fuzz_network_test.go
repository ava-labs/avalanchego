package simplex_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/simplex/testutil/random_network"
)

func TestNetworkSimpleFuzz(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("keeping logs in %s", tmpDir)
			return
		}
		_ = os.RemoveAll(tmpDir)
	})

	for i := 0; i < 10; i++ {
		t.Run("", func(t *testing.T) {
			config := random_network.DefaultFuzzConfigWithDir(t, tmpDir, i)
			config.RandomSeed = time.Now().UnixMilli()
			network := random_network.NewNetwork(config, t)
			network.Run()
			network.PrintStatus()
		})
	}
}
