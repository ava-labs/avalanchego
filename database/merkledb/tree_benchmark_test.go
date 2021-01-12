package merkledb

import (
	"crypto/rand"
	mrand "math/rand"
	"testing"
)

func CreateRandomValues(valueCount int) []TestStruct {
	var tests []TestStruct
	added := map[string]bool{}

	for i := 0; i < valueCount; i++ {
		key := make([]byte, mrand.Intn(31)+1) // #nosec G404
		val := make([]byte, mrand.Intn(31)+1) // #nosec G404
		_, _ = rand.Read(key)
		_, _ = rand.Read(val)

		keyString := string(key)
		if _, ok := added[keyString]; ok {
			i--
			continue
		}

		added[keyString] = true
		tests = append(tests, struct {
			key   []byte
			value []byte
		}{key: key, value: val})
	}

	return tests
}

func BenchmarkTree_Put(b *testing.B) {
	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test100k_Put", CreateRandomValues(100000)},
		// this takes a lot of time removed from the CI
		{"test1M_Put_Del", CreateRandomValues(1000000)},
		// {"test10M_Put_Del", CreateRandomValues(10000000)},
	}

	for _, test := range tests {
		tree := NewMemoryTree()
		b.Run(test.name, func(b *testing.B) {
			b.ResetTimer()

			for _, test := range test.data {
				_ = tree.Put(test.key, test.value)
			}
		})
	}
}

func BenchmarkTree_Del(b *testing.B) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test100k_Put_Del", CreateRandomValues(100000)},
		// this takes a lot of time removed from the CI
		{"test1M_Put_Del", CreateRandomValues(1000000)},
		// {"test10M_Put_Del", CreateRandomValues(10000000)},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			tree := NewMemoryTree()
			b.ResetTimer()
			for _, test := range test.data {
				_ = tree.Put(test.key, test.value)
			}

			for _, test := range test.data {
				if !tree.Del(test.key) {
					b.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
				}
			}
		})
	}
}
