package merkledb

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"

	"github.com/ava-labs/avalanchego/database"
)

// NewMemoryTree returns a new instance of the Tree with a in-memoryDB
func NewMemoryTree() *Tree {
	return NewTree(memdb.New())
}

func HardCloseDB(t *Tree) error {
	t.Close()
	return getDatabase(t.persistence).(*versiondb.Database).GetDatabase().Close()
}

// NewLevelTree returns a new instance of the Tree with a in-memoryDB
func NewLevelTree(file string) *Tree {
	db, err := leveldb.New(file, 0, 0, 0)
	if err != nil {
		panic(err)
	}
	return NewTree(db)
}

type TestStruct struct {
	Key   []byte
	Value []byte
}

type ScenarioTestStruct struct {
	Name      string
	PutData   []TestStruct
	GetData   []TestStruct
	DelData   []TestStruct
	ClearTree bool
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
			Name: "OneBranch",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
			},
		},
		{
			Name: "TwoBranches",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
				{Key: []byte{1, 2, 2}, Value: []byte{1, 2, 2}},
			},
		},
		{
			Name: "InsertDuplicateKV",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
				{Key: []byte{1, 2, 2}, Value: []byte{1, 2, 2}},
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
			},
		},
		{
			Name: "InsertDuplicateKDiffVal",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
				{Key: []byte{1, 2, 2}, Value: []byte{1, 2, 2}},
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 2}},
			},
			GetData: []TestStruct{
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
				{Key: []byte{1, 2, 2}, Value: []byte{1, 2, 2}},
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 2}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			tree := NewMemoryTree()

			for _, entry := range test.PutData {
				_ = tree.Put(entry.Key, entry.Value)
			}

			getData := test.PutData
			if test.GetData != nil {
				getData = test.GetData
			}
			for _, entry := range getData {
				val, err := tree.Get(entry.Key)
				if err != nil {
					t.Fatalf("unable to fetch %v - %v", entry, err)
				}
				if !bytes.Equal(entry.Value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.Value, val)
				}
			}
		})
	}
}

func TestTree_Del_Scenarios(t *testing.T) {

	tests := []ScenarioTestStruct{
		{
			Name: "One Branch Revert Deletion",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
			},
			DelData: []TestStruct{
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}},
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
			},
		},
		{
			Name: "Two Branch Revert Deletion",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}}, // one leaf
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}}, // one branch [1,1] - two leaves
				{Key: []byte{1, 2, 2}, Value: []byte{1, 2, 2}}, // two branches [1], [1,1] - three leaves
			},
			DelData: []TestStruct{
				{Key: []byte{1, 2, 2}, Value: []byte{1, 2, 2}}, // one branch [1,1] - two leaves
				{Key: []byte{1, 1, 2}, Value: []byte{1, 1, 2}}, // one leaf
				{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}}, // empty
			},
		},
		{
			Name: "Remove middle Branch",
			PutData: []TestStruct{
				{Key: []byte{1, 1, 1, 1}, Value: []byte{1, 1, 1, 1}},
				{Key: []byte{1, 1, 1, 2}, Value: []byte{1, 1, 1, 1}}, // has 1 branch at 1,1,1
				{Key: []byte{1, 1, 2, 0}, Value: []byte{1, 1, 1, 1}}, // has another branch at 1,1
				{Key: []byte{1, 1, 2, 1}, Value: []byte{1, 1, 1, 1}},
				{Key: []byte{1, 2, 0, 0}, Value: []byte{1, 1, 1, 1}}, // has another branch at 1
				{Key: []byte{1, 3, 3, 3}, Value: []byte{1, 1, 1, 1}},
			},
			DelData: []TestStruct{
				{Key: []byte{1, 2, 0, 0}, Value: []byte{1, 1, 1, 1}},
				{Key: []byte{1, 3, 3, 3}, Value: []byte{1, 1, 1, 1}}, // deletes the 1,1 branch -
				{Key: []byte{1, 1, 1, 1}, Value: []byte{1, 1, 1, 1}}, // TODO add a way to check # of branches + Nodes
				{Key: []byte{1, 1, 1, 2}, Value: []byte{1, 1, 1, 1}},
				{Key: []byte{1, 1, 2, 0}, Value: []byte{1, 1, 1, 1}},
				{Key: []byte{1, 1, 2, 1}, Value: []byte{1, 1, 1, 1}},
			},
		},
		{
			Name: "Shared Nibbles",
			PutData: []TestStruct{
				{Key: []byte{17, 17, 1}, Value: []byte{17, 17, 1}},
				{Key: []byte{17, 17, 2}, Value: []byte{17, 17, 2}},
				{Key: []byte{17, 1, 1}, Value: []byte{17, 1, 1}},
				{Key: []byte{17, 1, 2}, Value: []byte{17, 1, 2}},
				{Key: []byte{17, 1, 3}, Value: []byte{17, 1, 3}},
			},
			DelData: []TestStruct{
				{Key: []byte{17, 17, 1}, Value: []byte{17, 17, 1}},
				{Key: []byte{17, 17, 2}, Value: []byte{17, 17, 2}},
				{Key: []byte{17, 1, 1}, Value: []byte{17, 1, 1}},
				{Key: []byte{17, 1, 2}, Value: []byte{17, 1, 2}},
				{Key: []byte{17, 1, 3}, Value: []byte{17, 1, 3}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			tree := NewMemoryTree()

			for _, entry := range test.PutData {
				_ = tree.Put(entry.Key, entry.Value)
			}

			for _, entry := range test.PutData {
				val, err := tree.Get(entry.Key)
				if err != nil {
					t.Fatalf("unable to fetch %v - %v", entry, err)
				}
				if !bytes.Equal(entry.Value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.Value, val)
				}
			}

			for _, entry := range test.DelData {
				err := tree.Delete(entry.Key)
				if err != nil {
					t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.Key)
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
