package merkledb

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/database"
)

func PickRandomKey(list []TestStruct) (TestStruct, []TestStruct) {
	if len(list) == 0 {
		return TestStruct{}, []TestStruct{}
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		fmt.Println("error:", err)
		return TestStruct{}, nil
	}
	position := n.Uint64()
	test := list[position]
	list[len(list)-1], list[position] = list[position], list[len(list)-1]
	return test, list[:len(list)-1]
}

func TestTreeConsistency_PutGetDel(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"testConsistency-1k-PutGetDel", CreateRandomValues(1000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := newMemoryTree()

			putAndTestRoot(t, tree, test.data)

			getTest(t, tree, test.data)

			delAndTestRoot(t, tree, test.data)

			checkDatabaseItems(t, tree)
		})
	}
}

func TestTreeConsistency_PutGetClear(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"testConsistency-1k-PutGetDel", CreateRandomValues(1000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := newMemoryTree()

			putAndTestRoot(t, tree, test.data)

			getTest(t, tree, test.data)

			clearTest(t, tree)

			checkDatabaseItems(t, tree)
		})
	}
}

func TestTreeConsistencyStorage_PutGetDel(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"testConsistencyStorage-1k-PutGetDel", CreateRandomValues(1000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tree := newLevelTree(tmpDir)

			putAndTestRoot(t, tree, test.data)
			err := hardCloseDB(tree)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree2 := newLevelTree(tmpDir)
			getTest(t, tree2, test.data)
			err = hardCloseDB(tree2)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree3 := newLevelTree(tmpDir)
			delAndTestRoot(t, tree3, test.data)
			checkDatabaseItems(t, tree3)
			err = hardCloseDB(tree3)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree4 := newLevelTree(tmpDir)
			checkDatabaseItems(t, tree4)
			err = hardCloseDB(tree4)
			if err != nil {
				t.Fatal("Error closing the db")
			}
		})
	}
}

func putAndTestRoot(t *testing.T, tree *Tree, data []TestStruct) {
	var lastRootHash []byte
	for _, entry := range data {
		_ = tree.Put(entry.Key, entry.Value)

		rootHash, err := tree.Root()
		if err != nil {
			t.Fatalf("unable to fetch tree.Root() : %v", err)
		}

		if bytes.Equal(lastRootHash, rootHash) {
			t.Fatal("Root Hash didn't change after insertion")
		}
		lastRootHash, _ = tree.Root()
	}
}

func getTest(t *testing.T, tree *Tree, data []TestStruct) {
	stop := false
	var entry TestStruct
	testList := data

	for !stop {
		entry, testList = PickRandomKey(testList)
		if len(testList) == 0 {
			stop = true
		}

		val, err := tree.Get(entry.Key)

		if err != nil {
			t.Fatalf("value not found in the tree - %v - %v", entry.Key, err)
		}
		if !bytes.Equal(val, entry.Value) {
			t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", entry.Key, entry.Value, val)
		}
	}
}

func delAndTestRoot(t *testing.T, tree *Tree, data []TestStruct) {
	stop := false
	var entry TestStruct
	testList := data

	for !stop {
		entry, testList = PickRandomKey(testList)
		if len(testList) == 0 {
			stop = true
		}
		lastRootHash, _ := tree.Root()

		if err := tree.Delete(entry.Key); err != nil {
			i := 0
			for _, val := range data {
				if bytes.Equal(entry.Key, val.Key) {
					i++
					fmt.Printf("k: %v, v: %v\n", val.Key, val.Value)
				}
			}
			fmt.Printf("Number of times val exists: %d\n", i)
			t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.Key)
		}

		rootHash, err := tree.Root()
		if err != nil {
			t.Fatalf("unable to fetch tree.Root() : %v", err)
		}

		if bytes.Equal(lastRootHash, rootHash) {
			fmt.Printf("Deleted key: %v\n", entry.Key)
			fmt.Printf("lastRootHash: %v\n tree.Root: %v\n", lastRootHash, rootHash)
			t.Fatal("Root Hash didn't change after deletion")
		}
	}
}

func clearTest(t *testing.T, tree *Tree) {
	err := tree.rootNode.Clear()
	if err != nil {
		t.Fatal(err)
	}
}

func checkDatabaseItems(t *testing.T, tree *Tree) {

	db := getDatabase(tree.persistence)
	iterator := db.NewIterator()
	count := 0
	for iterator.Next() {
		var node Node

		nodeBytes, _ := getDatabase(tree.persistence).Get(iterator.Key())

		switch p := tree.persistence.(type) {
		case *TreePersistence:
			_, _ = p.codec.Unmarshal(nodeBytes, &node)
		case *ForestPersistence:
			_, _ = p.codec.Unmarshal(nodeBytes, &node)
		}

		fmt.Printf("Lingering - %v\n", node)

		count++
	}
	if count > 0 {
		t.Fatalf("Database is not empty - Number of Items: %d", count)
	}
}

func getDatabase(persistence Persistence) database.Database {
	switch p := persistence.(type) {
	case *ForestPersistence:
		return p.db
	case *TreePersistence:
		return p.db
	default:
		panic("unknown persistence type")
	}
}
