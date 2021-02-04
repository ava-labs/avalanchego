package merkledb

import (
	"testing"
)

func BenchmarkForest_Put(b *testing.B) {
	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_Put", CreateRandomValues(10000)},
		// {"test100k_Put", CreateRandomValues(100000)},
		// {"test1M_Put", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		forest := NewLevelForest(tmpDir)
		tree, _ := forest.CreateEmptyTree(0)

		b.Run(test.name, func(b *testing.B) {
			b.ResetTimer()

			for _, test := range test.data {
				_ = tree.Put(test.Key, test.Value)
			}
		})
		_ = HardCloseDB(tree)
	}
}

func BenchmarkForest_PutBatch(b *testing.B) {
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
		forest := NewLevelForest(tmpDir)
		tree, _ := forest.CreateEmptyTree(0)
		batcher := NewBatch(tree)

		b.Run(test.name, func(b *testing.B) {
			b.ResetTimer()

			for _, test := range test.data {
				_ = batcher.Put(test.Key, test.Value)
			}
			_ = batcher.Write()
		})
		_ = HardCloseDB(tree)
	}
}

func BenchmarkForestSecondTree_Put(b *testing.B) {
	tests := []struct {
		name  string
		data  []TestStruct
		data2 []TestStruct
	}{
		{"test10k_Put", CreateRandomValues(10000), CreateRandomValues(10000)},
		// {"test100k_Put", CreateRandomValues(100000), CreateRandomValues(100000)},
		// {"test1M_Put", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		forest := NewMemoryForest()
		tree, _ := forest.CreateEmptyTree(0)

		for _, test := range test.data {
			_ = tree.Put(test.Key, test.Value)
		}

		treeCopy, err := forest.Copy(0, 1)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for _, test2 := range test.data2 {
			_ = treeCopy.Put(test2.Key, test2.Value)
		}
	}
}

func BenchmarkForestSecondTree_PutBatch(b *testing.B) {
	tests := []struct {
		name  string
		data  []TestStruct
		data2 []TestStruct
	}{
		{"test10k_PutBatch", CreateRandomValues(10000), CreateRandomValues(10000)},
		{"test100k_PutBatch", CreateRandomValues(100000), CreateRandomValues(100000)},
		// {"test1M_Put", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		forest := NewLevelForest(tmpDir)
		tree, _ := forest.CreateEmptyTree(0)

		b.Run(test.name, func(b *testing.B) {
			for _, test := range test.data {
				_ = tree.Put(test.Key, test.Value)
			}

			treeCopy, _ := forest.Copy(0, 1)
			batcher := NewBatch(treeCopy)
			b.ResetTimer()
			for _, test := range test.data2 {
				_ = batcher.Put(test.Key, test.Value)
			}
			_ = batcher.Write()
		})
		_ = HardCloseDB(tree)
	}
}

func BenchmarkForest_Get(b *testing.B) {
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
		forest := NewLevelForest(tmpDir)
		tree, _ := forest.CreateEmptyTree(0)
		batchTree := NewBatch(tree)

		b.Run(test.name, func(b *testing.B) {
			for _, entry := range test.data {
				_ = batchTree.Put(entry.Key, entry.Value)
			}
			_ = batchTree.Write()

			b.ResetTimer()
			for _, entry := range test.data {
				_, err := tree.Get(entry.Key)

				if err != nil {
					b.Fatalf("value not found in the tree - %v - %v", entry.Key, err)
				}
			}
		})
		_ = HardCloseDB(tree)
	}
}

func BenchmarkForest_Del(b *testing.B) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_Del", CreateRandomValues(10000)},
		// {"test100k_Del", CreateRandomValues(100000)},
		// {"test1M_Del", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		forest := NewLevelForest(tmpDir)
		tree, _ := forest.CreateEmptyTree(0)

		b.Run(test.name, func(b *testing.B) {
			for _, test := range test.data {
				_ = tree.Put(test.Key, test.Value)
			}

			b.ResetTimer()
			for _, entry := range test.data {
				err := tree.Delete(entry.Key)

				if err != nil {
					b.Fatalf("value not deleted in the tree as it was not found- %v", entry.Key)
				}
			}
		})
		_ = HardCloseDB(tree)
	}
}

func BenchmarkForest_DelBatcher(b *testing.B) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k_DelBatcher", CreateRandomValues(10000)},
		// {"test100k_DelBatcher", CreateRandomValues(100000)},
		// {"test1M_Del", CreateRandomValues(1000000)},
	}

	for _, test := range tests {
		tmpDir := b.TempDir()
		forest := NewLevelForest(tmpDir)
		tree, _ := forest.CreateEmptyTree(0)
		batcher := NewBatch(tree)

		b.Run(test.name, func(b *testing.B) {
			for _, test := range test.data {
				_ = batcher.Put(test.Key, test.Value)
			}
			_ = batcher.Write()

			b.ResetTimer()
			for _, entry := range test.data {
				err := batcher.Delete(entry.Key)

				if err != nil {
					b.Fatalf("value not deleted in the tree as it was not found- %v", entry.Key)
				}
			}
			_ = batcher.Write()

		})
		_ = HardCloseDB(tree)
	}
}
