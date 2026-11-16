package simplex_test

import (
	"testing"
	"time"

	"github.com/ava-labs/simplex/testutil/random_network"
)

func TestNetworkSimpleFuzz(t *testing.T) {
	for i := 0; i < 10; i++ {
		t.Run("", func(t *testing.T) {
			config := random_network.DefaultFuzzConfig()
			config.RandomSeed = time.Now().UnixMilli()
			network := random_network.NewNetwork(config, t)
			network.Run()
			network.PrintStatus()
		})
	}
}
