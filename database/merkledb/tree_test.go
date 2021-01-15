package merkledb

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/database"
)

type TestStruct struct {
	key   []byte
	value []byte
}

type ScenarioTestStruct struct {
	name    string
	putData []TestStruct
	delData []TestStruct
}

func TestTree_Put(t *testing.T) {
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

	for _, test := range tests {
		val, err := tree.Get(test.key)
		if err != nil {
			t.Fatalf("value not found in the tree - %v - %v", test.key, err)
		}
		if !bytes.Equal(val, test.value) {
			t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", test.key, test.value, val)
		}
	}
}

func TestTree_PutVariableKeys(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{1, 1, 1, 1}, []byte{1, 1, 1, 1}},
		{[]byte{1, 1, 1, 1, 2}, []byte{1, 1, 1, 1, 2}},
		{[]byte{1, 1, 1, 2}, []byte{1, 1, 1, 2}},
		{[]byte{1, 1, 1, 1, 3}, []byte{1, 1, 1, 1, 3}},
	}

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
	}

	for _, test := range tests {
		val, err := tree.Get(test.key)
		if err != nil {
			t.Fatalf("value not found in the tree - %v - %v", test.key, err)
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

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
	}

	for _, test := range tests {
		val, err := tree.Get(test.key)
		if err != nil {
			t.Fatalf("value not found in the tree - %v - %v", test.key, err)
		}
		if !bytes.Equal(val, test.value) {
			t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", test.key, test.value, val)
		}
	}

	for _, test := range tests {
		err := tree.Delete(test.key)
		if err != nil {
			t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, test.key)
		}
	}

}

func TestTree_DelVariableKeys(t *testing.T) {
	tests := []struct {
		key   []byte
		value []byte
	}{
		{[]byte{0, 244, 110, 7}, []byte{30, 244, 110, 7}},
		{[]byte{75, 186, 40, 9, 2}, []byte{175, 186, 40, 9, 2}},
		{[]byte{83, 189, 22, 22}, []byte{183, 189, 22, 22}},
		{[]byte{5, 210, 129, 94}, []byte{85, 210, 129, 94}},
		{[]byte{60, 158, 96, 67}, []byte{160, 158, 96, 67}},
		{[]byte{36, 154, 165, 25}, []byte{136, 154, 165, 25}},
		{[]byte{36, 154}, []byte{136, 154}},
		{[]byte{36, 154, 20}, []byte{136, 154, 20}},
		{[]byte{64, 130, 11, 38}, []byte{164, 130, 11, 38}},
		{[]byte{24, 157, 35, 12}, []byte{124, 157, 35, 12}},
		{[]byte{7, 188, 148, 22}, []byte{77, 188, 148, 22}},
	}

	tree := NewMemoryTree()

	for _, test := range tests {
		_ = tree.Put(test.key, test.value)
	}

	for _, test := range tests {
		err := tree.Delete(test.key)
		if err != nil {
			t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, test.key)
		}
	}

}

func TestTree_Put_Scenarios(t *testing.T) {

	tests := []ScenarioTestStruct{
		{
			name: "OneBranch",
			putData: []TestStruct{
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}},
			},
		},
		{
			name: "TwoBranches",
			putData: []TestStruct{
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}},
				{key: []byte{1, 2, 2}, value: []byte{1, 2, 2}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewMemoryTree()

			for _, entry := range test.putData {
				_ = tree.Put(entry.key, entry.value)
			}

			for _, entry := range test.putData {
				val, err := tree.Get(entry.key)
				if err != nil {
					t.Fatalf("unable to fetch %v - %v", entry, err)
				}
				if !bytes.Equal(entry.value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.value, val)
				}
			}
		})
	}
}

func TestTree_Del_Scenarios(t *testing.T) {

	tests := []ScenarioTestStruct{
		{
			name: "One Branch Revert Deletion",
			putData: []TestStruct{
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}},
			},
			delData: []TestStruct{
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}},
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
			},
		},
		{
			name: "Two Branch Revert Deletion",
			putData: []TestStruct{
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}}, // one leaf
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}}, // one branch [1,1] - two leaves
				{key: []byte{1, 2, 2}, value: []byte{1, 2, 2}}, // two branches [1], [1,1] - three leaves
			},
			delData: []TestStruct{
				{key: []byte{1, 2, 2}, value: []byte{1, 2, 2}}, // one branch [1,1] - two leaves
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}}, // one leaf
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}}, // empty
			},
		},
		{
			name: "Remove middle Branch",
			putData: []TestStruct{
				{key: []byte{1, 1, 1, 1}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 1, 1, 2}, value: []byte{1, 1, 1, 1}}, // has 1 branch at 1,1,1
				{key: []byte{1, 1, 2, 0}, value: []byte{1, 1, 1, 1}}, // has another branch at 1,1
				{key: []byte{1, 1, 2, 1}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 2, 0, 0}, value: []byte{1, 1, 1, 1}}, // has another branch at 1
				{key: []byte{1, 3, 3, 3}, value: []byte{1, 1, 1, 1}},
			},
			delData: []TestStruct{
				{key: []byte{1, 2, 0, 0}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 3, 3, 3}, value: []byte{1, 1, 1, 1}}, // deletes the 1,1 branch -
				{key: []byte{1, 1, 1, 1}, value: []byte{1, 1, 1, 1}}, // TODO add a way to check # of branches + Nodes
				{key: []byte{1, 1, 1, 2}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 1, 2, 0}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 1, 2, 1}, value: []byte{1, 1, 1, 1}},
			},
		},
		{
			name: "Shared Nibbles",
			putData: []TestStruct{
				{key: []byte{17, 17, 1}, value: []byte{17, 17, 1}},
				{key: []byte{17, 17, 2}, value: []byte{17, 17, 2}},
				{key: []byte{17, 1, 1}, value: []byte{17, 1, 1}},
				{key: []byte{17, 1, 2}, value: []byte{17, 1, 2}},
				{key: []byte{17, 1, 3}, value: []byte{17, 1, 3}},
			},
			delData: []TestStruct{
				{key: []byte{17, 17, 1}, value: []byte{17, 17, 1}},
				{key: []byte{17, 17, 2}, value: []byte{17, 17, 2}},
				{key: []byte{17, 1, 1}, value: []byte{17, 1, 1}},
				{key: []byte{17, 1, 2}, value: []byte{17, 1, 2}},
				{key: []byte{17, 1, 3}, value: []byte{17, 1, 3}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewMemoryTree()

			for _, entry := range test.putData {
				_ = tree.Put(entry.key, entry.value)
			}

			for _, entry := range test.putData {
				val, err := tree.Get(entry.key)
				if err != nil {
					t.Fatalf("unable to fetch %v - %v", entry, err)
				}
				if !bytes.Equal(entry.value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.value, val)
				}
			}

			for _, entry := range test.delData {
				err := tree.Delete(entry.key)
				if err != nil {
					t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.key)
				}
			}
		})
	}
}

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		treeA := NewMemoryTree()
		treeB := NewMemoryTree()
		test(t, treeA)
		test(t, treeB)
	}
}
