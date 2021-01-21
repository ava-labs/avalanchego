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
		// {"test1M_Put", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		tree := NewLevelTree(tmpDir)

		b.Run(test.name, func(b *testing.B) {
			b.ResetTimer()

			for _, test := range test.data {
				_ = tree.Put(test.key, test.value)
			}
		})
		_ = tree.Close()
	}
}

func BenchmarkTree_PutBatch(b *testing.B) {
	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_PutBatch", CreateRandomValues(10000)},
		{"test100k_PutBatch", CreateRandomValues(100000)},
		// {"test1M_Put", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		tree := NewLevelTree(tmpDir)
		batcher := NewBatch(tree)

		b.Run(test.name, func(b *testing.B) {
			b.ResetTimer()

			for _, test := range test.data {
				_ = batcher.Put(test.key, test.value)
			}
			_ = batcher.Write()
		})
		_ = tree.Close()
	}
}

func BenchmarkTree_Get(b *testing.B) {
	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_Get", CreateRandomValues(10000)},
		{"test100k_Get", CreateRandomValues(100000)},
		// {"test1M_Put", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		tree := NewLevelTree(tmpDir)
		batchTree := NewBatch(tree)

		b.Run(test.name, func(b *testing.B) {
			for _, entry := range test.data {
				_ = batchTree.Put(entry.key, entry.value)
			}
			_ = batchTree.Write()

			b.ResetTimer()
			for _, entry := range test.data {
				_, err := tree.Get(entry.key)

				if err != nil {
					tree.PrintTree()
					b.Fatalf("value not found in the tree - %v - %v", entry.key, err)
				}
			}
		})
		_ = tree.Close()
	}
}

func BenchmarkTree_Del(b *testing.B) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_Del", CreateRandomValues(10000)},
		{"test100k_Del", CreateRandomValues(100000)},
		// {"test1M_Del", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		tree := NewLevelTree(tmpDir)

		b.Run(test.name, func(b *testing.B) {
			for _, test := range test.data {
				_ = tree.Put(test.key, test.value)
			}

			b.ResetTimer()
			for _, entry := range test.data {
				err := tree.Delete(entry.key)

				if err != nil {
					b.Fatalf("value not deleted in the tree as it was not found- %v", entry.key)
				}
			}
		})
		_ = tree.Close()
	}
}

func BenchmarkTree_DelBatcher(b *testing.B) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_DelBatcher", CreateRandomValues(10000)},
		{"test100k_DelBatcher", CreateRandomValues(100000)},
		// {"test1M_Del", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		tree := NewLevelTree(tmpDir)
		batcher := NewBatch(tree)

		b.Run(test.name, func(b *testing.B) {
			for _, test := range test.data {
				_ = batcher.Put(test.key, test.value)
			}
			_ = batcher.Write()

			b.ResetTimer()
			for _, entry := range test.data {
				err := batcher.Delete(entry.key)

				if err != nil {
					b.Fatalf("value not deleted in the tree as it was not found- %v", entry.key)
				}
			}
			_ = batcher.Write()

		})
		_ = tree.Close()
	}
}
