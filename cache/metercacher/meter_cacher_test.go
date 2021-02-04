package metercacher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
)

func TestInterface(t *testing.T) {
	for _, test := range cache.CacherTests {
		cache := &cache.LRU{Size: 1}
		c, err := New("", prometheus.NewRegistry(), cache)
		if err != nil {
			t.Fatal(err)
		}

		test(t, c)
	}
}
