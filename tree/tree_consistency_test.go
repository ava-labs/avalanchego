package tree

import (
	"math/rand"
	"testing"
)

func PickRandomKey(list []TestStruct) (TestStruct, []TestStruct) {
	position := rand.Intn(len(list))
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
			for _, entry := range test.data {
				tree.Put(entry.key, entry.value)
			}

			for entry, testList := PickRandomKey(test.data); len(testList) > 0; entry, testList = PickRandomKey(testList) {
				if !tree.Del(entry.key) {
					tree.PrintTree()
					t.Fatalf("value not deleted in the tree as it was not found- %v", entry.key)
				}
			}
		})
	}
}
