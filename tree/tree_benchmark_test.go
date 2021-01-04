package tree

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

	tree := NewTree()
	test100k := CreateRandomValues(100000)
	test1M := CreateRandomValues(1000000)
	test10M := CreateRandomValues(10000000)

	b.Run("test100k", func(b *testing.B) {
		b.ResetTimer()

		for _, test := range test100k {
			_ = tree.Put(test.key, test.value)
		}
	})

	b.Run("test1M", func(b *testing.B) {
		b.ResetTimer()
		for _, test := range test1M {
			_ = tree.Put(test.key, test.value)
		}
	})

	b.Run("test10M", func(b *testing.B) {
		b.ResetTimer()
		for _, test := range test10M {
			_ = tree.Put(test.key, test.value)
		}
	})
}

func BenchmarkTree_Del(b *testing.B) {

	test100k := CreateRandomValues(100000)
	test1M := CreateRandomValues(1000000)
	test10M := CreateRandomValues(10000000)

	b.Run("test100k_Put_Del", func(b *testing.B) {
		tree := NewTree()
		b.ResetTimer()
		for _, test := range test100k {
			_ = tree.Put(test.key, test.value)
		}

		for _, test := range test100k {
			if !tree.Del(test.key) {
				b.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
			}
		}
	})

	b.Run("test1M_Put_Del", func(b *testing.B) {
		b.ResetTimer()
		tree := NewTree()
		for _, test := range test1M {
			_ = tree.Put(test.key, test.value)
		}

		for _, test := range test1M {
			if !tree.Del(test.key) {
				b.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
			}
		}
	})

	b.Run("test10M_Put_Del", func(b *testing.B) {
		b.ResetTimer()
		tree := NewTree()
		for _, test := range test10M {
			_ = tree.Put(test.key, test.value)
		}

		for _, test := range test10M {
			if !tree.Del(test.key) {
				tree.PrintTree()
				b.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
			}
		}
	})
}
