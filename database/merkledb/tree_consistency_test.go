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

func TestTreeConsistency_PutGetDel(t *testing.T) {

	tests := []struct {
		name      string
		getMethod bool
		data      []TestStruct
	}{
		{"test10k-Get", true, CreateRandomValues(10000)},
		{"test50k-Get", true, CreateRandomValues(100000)},
		{"test10k-GetTraverse", false, CreateRandomValues(10000)},
		// {"test100k-GetTraverse", false, CreateRandomValues(100000)},
		// this takes a lot of time removed from the CI
		// {"test1M", CreateRandomValues(1000000)},
		// {"test5M", CreateRandomValues(5000000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewMemoryTree()
			added := map[string]bool{}

			var lastRootHash []byte
			for _, entry := range test.data {
				_ = tree.Put(entry.key, entry.value)
				added[string(entry.key)] = true
				if bytes.Equal(lastRootHash, tree.Root()) {
					tree.PrintTree()
					t.Fatal("Root Hash didn't change after insertion")
				}
				lastRootHash = tree.Root()
			}

			lastRootHash = []byte{}
			for entry, testList := PickRandomKey(test.data); len(testList) > 0; entry, testList = PickRandomKey(testList) {
				var val []byte
				var err error
				if test.getMethod {
					val, err = tree.Get(entry.key)
				} else {
					val, err = tree.GetTraverse(entry.key)
				}

				if err != nil {
					t.Fatalf("value not found in the tree - %v - %v", entry.key, err)
				}
				if !bytes.Equal(val, entry.value) {
					t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", entry.key, entry.value, val)
				}

				if !tree.Del(entry.key) {
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

				if bytes.Equal(lastRootHash, tree.Root()) {
					fmt.Printf("Deleted key: %v\n", entry.key)
					fmt.Printf("lastRootHash: %v\n tree.Root: %v\n", lastRootHash, tree.Root())
					t.Fatal("Root Hash didn't change after deletion")
				}

				lastRootHash = tree.Root()
			}
		})
	}
}
