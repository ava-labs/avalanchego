package merkledb

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
)

func PickRandomKey(list []TestStruct) (TestStruct, []TestStruct) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		fmt.Println("error:", err)
		return TestStruct{}, nil
	}
	position := n.Int64()
	test := list[position]
	list[position] = list[len(list)-1]
	return test, list[:len(list)-1]
}

func TestTreeConsistency_Del(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test10k", CreateRandomValues(10000)},
		{"test100k", CreateRandomValues(100000)},
		// this takes a lot of time removed from the CI
		// {"test1M", CreateRandomValues(1000000)},
		// {"test5M", CreateRandomValues(5000000)},
	}

	var lastRootHash []byte
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewMemoryTree()
			added := map[string]bool{}
			for _, entry := range test.data {
				_ = tree.Put(entry.key, entry.value)
				added[string(entry.key)] = true
				if bytes.Equal(lastRootHash, tree.Root()) {
					t.Fatal("Root Hash didn't change after insertion")
				}
				lastRootHash = tree.Root()

			}

			for entry, testList := PickRandomKey(test.data); len(testList) > 0; entry, testList = PickRandomKey(testList) {
				if !tree.Del(entry.key) {

					if bytes.Equal(lastRootHash, tree.Root()) {
						t.Fatal("Root Hash didn't change after deletion")
					}
					lastRootHash = tree.Root()

					i := 0
					for _, val := range test.data {
						if bytes.Equal(entry.key, val.key) {
							i++
							fmt.Printf("k: %v, v: %v\n", val.key, val.value)
						}
					}
					fmt.Printf("Number of times val exists: %d\n", i)
					fmt.Printf("Key added: %v\n", string(entry.key))
					t.Fatalf("value not deleted in the tree as it was not found- %v", entry.key)
				}
			}
		})
	}
}
