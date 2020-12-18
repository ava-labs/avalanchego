package tree

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
		{"test100k", CreateRandomValues(100000)},
		{"test1M", CreateRandomValues(1000000)},
		{"test10M", CreateRandomValues(10000000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewTree()
			added := map[string]bool{}
			for _, entry := range test.data {
				tree.Put(entry.key, entry.value)
				added[string(entry.key)] = true
			}

			for entry, testList := PickRandomKey(test.data); len(testList) > 0; entry, testList = PickRandomKey(testList) {
				if !tree.Del(entry.key) {
					//tree.PrintTree()
					i := 0
					for _, val := range test.data {
						if bytes.Equal(entry.key, val.key) {
							i++
						}
					}
					fmt.Printf("Number of times val exists: %d\n", i)
					fmt.Printf("Value was added: %v\n", added[string(entry.key)])
					t.Fatalf("value not deleted in the tree as it was not found- %v", entry.key)
				}
			}
		})
	}
}
