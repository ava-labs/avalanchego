package tree

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
)

type TestStruct struct {
	key   []byte
	value []byte
}

func CreateRandomValues(valueCount int) []TestStruct {
	var tests []TestStruct
	for i := 0; i < valueCount; i++ {
		bs := make([]byte, 4)
		val := make([]byte, 4)
		binary.LittleEndian.PutUint32(bs, uint32(rand.Int31()))
		binary.LittleEndian.PutUint32(val, uint32(rand.Int31()))

		tests = append(tests, struct {
			key   []byte
			value []byte
		}{key: bs, value: val})
	}

	return tests
}

func BenchmarkCalculate(b *testing.B) {

	tree := NewTree()

	test100k := CreateRandomValues(100000)
	test1M := CreateRandomValues(1000000)
	test10M := CreateRandomValues(1000000)

	b.Run("test100k", func(b *testing.B) {
		b.ResetTimer()
		for _, test := range test100k {
			fmt.Println(test.key)
			tree.Put(test.key, test.value)
		}

		for _, test := range test100k {
			deleted := tree.Del(test.key)
			if !deleted {
				b.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
			}
		}
	})
	b.Run("test1M", func(b *testing.B) {
		b.ResetTimer()
		for _, test := range test1M {
			tree.Put(test.key, test.value)
		}
	})
	b.Run("test10M", func(b *testing.B) {
		b.ResetTimer()
		for _, test := range test10M {
			tree.Put(test.key, test.value)
		}
	})

}
