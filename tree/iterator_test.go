package tree

import (
	"bytes"
	"testing"
)

func TestNewIterator(t *testing.T) {
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

	iter := NewIterator(tree)
	if iter.Key() != nil && iter.Value() != nil && iter.Error() != nil {
		t.Fatal("initial values are incorrect")
	}

	i := 0
	for iter.Next() {
		if !bytes.Equal(iter.Key(), tests[i].key) {
			t.Fatalf("wrong key fetched, expected: %v, got: %v", tests[i].key, iter.Key())
		}
		if !bytes.Equal(iter.Value(), tests[i].value) {
			t.Fatalf("wrong value fetched, expected: %v, got: %v", tests[i].value, iter.Value())
		}

		i++
	}

}
