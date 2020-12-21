package tree

import (
	"bytes"
	"fmt"
	"testing"
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
			tree := NewTree()

			for _, entry := range test.putData {
				tree.Put(entry.key, entry.value)
			}

			for _, entry := range test.putData {
				val, ok := tree.Get(entry.key)
				if !ok {
					t.Fatalf("unable to fetch %v", entry)
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
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}},
				{key: []byte{1, 2, 2}, value: []byte{1, 2, 2}},
			},
			delData: []TestStruct{
				{key: []byte{1, 2, 2}, value: []byte{1, 2, 2}},
				{key: []byte{1, 1, 2}, value: []byte{1, 1, 2}},
				{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
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
				{key: []byte{1, 1, 1, 1}, value: []byte{1, 1, 1, 1}}, // TODO add a way to check # of branches + nodes
				{key: []byte{1, 1, 1, 2}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 1, 2, 0}, value: []byte{1, 1, 1, 1}},
				{key: []byte{1, 1, 2, 1}, value: []byte{1, 1, 1, 1}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewTree()

			for _, entry := range test.putData {
				tree.Put(entry.key, entry.value)
			}

			for _, entry := range test.putData {
				val, ok := tree.Get(entry.key)
				if !ok {
					t.Fatalf("unable to fetch %v", entry)
				}
				if !bytes.Equal(entry.value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.value, val)
				}
			}

			for _, entry := range test.delData {
				ok := tree.Del(entry.key)
				if !ok {
					t.Fatalf("unable to delete %v", entry)
				}
			}
		})
	}
}

//func TestInterface(t *testing.T) {
//	for _, test := range database.Tests {
//		test(t, NewTree())
//	}
//}
