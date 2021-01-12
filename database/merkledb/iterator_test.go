package merkledb

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

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
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

func TestNewIteratorWithPrefix(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{1, 1, 1, 1}, []byte{1, 1, 1, 1}},
		{[]byte{1, 1, 1, 2}, []byte{1, 1, 1, 2}},
		{[]byte{1, 2, 3, 3}, []byte{1, 2, 3, 3}},
		{[]byte{1, 3, 3, 3}, []byte{1, 2, 3, 3}},
	}

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
	}

	iter := NewIteratorWithPrefix(tree, []byte{1, 2})
	if iter.Key() != nil && iter.Value() != nil && iter.Error() != nil {
		t.Fatal("initial values are incorrect")
	}

	i := 2
	for iter.Next() {
		if !bytes.Equal(iter.Key(), tests[i].key) {
			t.Fatalf("wrong key fetched, expected: %v, got: %v", tests[i].key, iter.Key())
		}
		if !bytes.Equal(iter.Value(), tests[i].value) {
			t.Fatalf("wrong value fetched, expected: %v, got: %v", tests[i].value, iter.Value())
		}

		i++
	}

	if i != 3 {
		t.Fatalf("iterated over too many values, expected: %d, got: %d", 3, i)
	}
}

func TestNewIteratorWithStart(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{1, 1, 1, 1}, []byte{1, 1, 1, 1}},
		{[]byte{1, 1, 1, 2}, []byte{1, 1, 1, 2}},
		{[]byte{1, 2, 3, 3}, []byte{1, 2, 3, 3}},
		{[]byte{1, 3, 3, 3}, []byte{1, 2, 3, 3}},
	}

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
	}

	iter := NewIteratorWithStart(tree, []byte{1, 2})
	if iter.Key() != nil && iter.Value() != nil && iter.Error() != nil {
		t.Fatal("initial values are incorrect")
	}

	i := 2
	for iter.Next() {
		if !bytes.Equal(iter.Key(), tests[i].key) {
			t.Fatalf("wrong key fetched, expected: %v, got: %v", tests[i].key, iter.Key())
		}
		if !bytes.Equal(iter.Value(), tests[i].value) {
			t.Fatalf("wrong value fetched, expected: %v, got: %v", tests[i].value, iter.Value())
		}

		i++
	}

	if i != 4 {
		t.Fatalf("iterated over too many values, expected: %d, got: %d", 4, i)
	}
}

func TestNewIteratorWithPrefixAndStart(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{0, 1, 1, 1}, []byte{0, 1, 1, 1}},
		{[]byte{1, 1, 1, 1}, []byte{1, 1, 1, 1}},
		{[]byte{1, 1, 1, 2}, []byte{1, 1, 1, 2}},
		{[]byte{1, 1, 1, 3}, []byte{1, 1, 1, 3}},
		{[]byte{1, 1, 2, 3}, []byte{1, 1, 2, 4}},
		{[]byte{1, 2, 3, 3}, []byte{1, 2, 3, 3}},
		{[]byte{1, 3, 3, 3}, []byte{1, 2, 3, 3}},
	}

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
	}

	iter := NewIteratorWithStartAndPrefix(tree, []byte{1, 1, 1, 2}, []byte{1, 1})
	if iter.Key() != nil && iter.Value() != nil && iter.Error() != nil {
		t.Fatal("initial values are incorrect")
	}

	i := 2
	for iter.Next() {
		if !bytes.Equal(iter.Key(), tests[i].key) {
			t.Fatalf("wrong key fetched, expected: %v, got: %v", tests[i].key, iter.Key())
		}
		if !bytes.Equal(iter.Value(), tests[i].value) {
			t.Fatalf("wrong value fetched, expected: %v, got: %v", tests[i].value, iter.Value())
		}

		i++
	}

	if i != 4 {
		t.Fatalf("iterated over too many values, expected: %d, got: %d", 4, i)
	}
}
