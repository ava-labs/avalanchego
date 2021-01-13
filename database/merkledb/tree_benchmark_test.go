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
		{"test10k_Put", CreateRandomValues(10000)},
		{"test100k_Put", CreateRandomValues(100000)},
		// this takes a lot of time removed from the CI
		// {"test1M_Put", CreateRandomValues(1000000)},
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

func BenchmarkTree_Get(b *testing.B) {
	tests := []struct {
		name     string
		traverse bool
		data     []TestStruct
	}{
		{"test10k_Put", false, CreateRandomValues(10000)},
		{"test10k_Put_Traverse", true, CreateRandomValues(10000)},
		{"test50k_Put", false, CreateRandomValues(50000)},
		{"test50k_Put_Traverse", true, CreateRandomValues(50000)},
		// this takes a lot of time removed from the CI
		// {"test1M_Put", CreateRandomValues(1000000)},
		// {"test10M_Put_Del", CreateRandomValues(10000000)},
	}

	for _, test := range tests {
		tree := NewMemoryTree()
		b.Run(test.name, func(b *testing.B) {
			for _, entry := range test.data {
				_ = tree.Put(entry.key, entry.value)
			}

			b.ResetTimer()
			for _, entry := range test.data {
				var err error

				if test.traverse {
					_, err = tree.Get(entry.key)
				} else {
					_, err = tree.GetTraverse(entry.key)
				}

				if err != nil {
					b.Fatalf("value not found in the tree - %v - %v", entry.key, err)
				}
			}
		})
	}
}

func BenchmarkTree_Del(b *testing.B) {

	tests := []struct {
		name     string
		traverse bool
		data     []TestStruct
	}{
		{"test10k_Put_Del", false, CreateRandomValues(10000)},
		{"test10k_Put_Del_Traverse", true, CreateRandomValues(10000)},
		{"test50k_Put_Del", false, CreateRandomValues(50000)},
		{"test50k_Put_Del_Traverse", true, CreateRandomValues(50000)},
		// this takes a lot of time removed from the CI
		// {"test1M_Put_Del", CreateRandomValues(1000000)},
		// {"test10M_Put_Del", CreateRandomValues(10000000)},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			tree := NewMemoryTree()
			for _, test := range test.data {
				_ = tree.Put(test.key, test.value)
			}

			b.ResetTimer()
			for _, entry := range test.data {
				var found bool
				if test.traverse {
					found = tree.DelTraverse(entry.key)
				} else {
					found = tree.Del(entry.key)
				}

				if !found {
					b.Fatalf("value not deleted in the tree as it was not found- %v", entry.key)
				}
			}
		})
	}
}
