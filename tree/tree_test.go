package tree

import (
	"bytes"
	"fmt"
	"testing"
)

func TestTree_Put(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{1, 1, 1, 1}, []byte{1, 1, 1, 1}},
		{[]byte{1, 1, 1, 2}, []byte{1, 1, 1, 2}},
		{[]byte{1, 2, 3, 3}, []byte{1, 2, 3, 3}},
	}

	tree := NewTree()

	for _, test := range tests {
		tree.Put(test.key, test.value)
	}

	tree.PrintTree()

	for _, test := range tests {
		val, found := tree.Get(test.key)
		if !found {
			t.Fatalf("value not found in the tree - %v", test.key)
		}
		if !bytes.Equal(val, test.value) {
			t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", test.key, test.value, val)
		}
	}
}

func TestTree_Del(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{0, 244, 110, 7}, []byte{30, 244, 110, 7}},
		{[]byte{75, 186, 40, 9}, []byte{175, 186, 40, 9}},
		{[]byte{83, 189, 22, 22}, []byte{183, 189, 22, 22}},
		{[]byte{5, 210, 129, 94}, []byte{85, 210, 129, 94}},
		{[]byte{60, 158, 96, 67}, []byte{160, 158, 96, 67}},
		{[]byte{36, 154, 165, 25}, []byte{136, 154, 165, 25}},
		{[]byte{64, 130, 11, 38}, []byte{164, 130, 11, 38}},
		{[]byte{24, 157, 35, 12}, []byte{124, 157, 35, 12}},
		{[]byte{7, 188, 148, 22}, []byte{77, 188, 148, 22}},
	}

	tree := NewTree()

	for _, test := range tests {
		tree.Put(test.key, test.value)
	}

	tree.PrintTree()
	fmt.Printf("Full Tree -\n\n")
	for _, test := range tests {
		deleted := tree.Del(test.key)
		if !deleted {
			t.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
		}

		tree.PrintTree()
		fmt.Printf("deleted - %v\n\n", test.key)
	}

}

func TestTree_PutDelPutDel(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{1, 1, 2}, []byte{1, 1, 2}},
		{[]byte{1, 2, 3}, []byte{1, 2, 3}},
		{[]byte{1, 2, 4}, []byte{1, 2, 4}},
		{[]byte{8, 4, 4}, []byte{8, 4, 4}},
		{[]byte{8, 3, 5}, []byte{8, 3, 5}},
		{[]byte{8, 4, 6}, []byte{8, 4, 6}},
	}

	tree := NewTree()

	for _, test := range tests {
		tree.Put(test.key, test.value)
	}

	tree.PrintTree()
	fmt.Printf("Full Tree -\n\n")
	for _, test := range tests {
		deleted := tree.Del(test.key)
		if !deleted {
			t.Fatalf("value not deleted in the tree as it was not found- %v", test.key)
		}

		tree.PrintTree()
		fmt.Printf("deleted - %v\n\n", test.key)
	}

}
