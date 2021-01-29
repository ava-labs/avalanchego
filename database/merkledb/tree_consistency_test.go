package merkledb

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
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
			tree := NewMemoryTree()

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
			tree := NewMemoryTree()

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
			tree := NewLevelTree(tmpDir)

			putAndTestRoot(t, tree, test.data)
			err := HardCloseDB(tree)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree2 := NewLevelTree(tmpDir)
			getTest(t, tree2, test.data)
			err = HardCloseDB(tree2)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree3 := NewLevelTree(tmpDir)
			delAndTestRoot(t, tree3, test.data)
			checkDatabaseItems(t, tree3)
			err = HardCloseDB(tree3)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree4 := NewLevelTree(tmpDir)
			checkDatabaseItems(t, tree4)
			err = HardCloseDB(tree4)
			if err != nil {
				t.Fatal("Error closing the db")
			}
		})
	}
}

func putAndTestRoot(t *testing.T, tree *Tree, data []TestStruct) {
	var lastRootHash []byte
	for _, entry := range data {
		_ = tree.Put(entry.key, entry.value)

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

		val, err := tree.Get(entry.key)

		if err != nil {
			t.Fatalf("value not found in the tree - %v - %v", entry.key, err)
		}
		if !bytes.Equal(val, entry.value) {
			t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", entry.key, entry.value, val)
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

		if err := tree.Delete(entry.key); err != nil {
			i := 0
			for _, val := range data {
				if bytes.Equal(entry.key, val.key) {
					i++
					fmt.Printf("k: %v, v: %v\n", val.key, val.value)
				}
			}
			fmt.Printf("Number of times val exists: %d\n", i)
			t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.key)
		}

		rootHash, err := tree.Root()
		if err != nil {
			t.Fatalf("unable to fetch tree.Root() : %v", err)
		}

		if bytes.Equal(lastRootHash, rootHash) {
			fmt.Printf("Deleted key: %v\n", entry.key)
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
	iterator := tree.persistence.GetDatabase().NewIterator()
	count := 0
	for iterator.Next() {
		fmt.Printf("Key: %x , Val: %v\n", iterator.Key(), iterator.Value())
		derp, _ := tree.persistence.GetDatabase().Get(iterator.Key())
		fmt.Println(derp)

		var node Node
		derp2, err := tree.persistence.(*TreePersistence).codec.Unmarshal(derp, &node)
		fmt.Println(derp2)
		fmt.Println(err)
		count++
	}
	if count > 0 {
		fmt.Println(count)
		t.Fatalf("Database is not empty - Number of Items: %d", count)
	}
}
