package merkledb

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
	"time"
)

func PickRandomKey(list []TestStruct) (TestStruct, []TestStruct) {
	if len(list) == 0 {
		fmt.Println("derp")
		return TestStruct{}, []TestStruct{}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		fmt.Println("error:", err)
		return TestStruct{}, nil
	}
	position := n.Uint64()
	test := list[position]
	list[position] = list[len(list)-1]
	return test, list[:len(list)-1]
}

func TestTreeConsistency_PutGetDel(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test1k-PutGetDel", CreateRandomValues(1000)},
		// {"test10k-PutGetDel", CreateRandomValues(10000)},
		// {"test100k-PutGetDel", CreateRandomValues(100000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := NewMemoryTree()
			added := map[string]bool{}

			var lastRootHash []byte
			for _, entry := range test.data {
				_ = tree.Put(entry.key, entry.value)
				added[string(entry.key)] = true

				rootHash, err := tree.Root()
				if err != nil {
					t.Fatalf("unable to fetch tree.Root() : %v", err)
				}

				if bytes.Equal(lastRootHash, rootHash) {
					t.Fatal("Root Hash didn't change after insertion")
				}
				lastRootHash, _ = tree.Root()
			}

			lastRootHash = []byte{}
			stop := false
			testList := test.data
			var entry TestStruct

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

				if tree.Delete(entry.key) != nil {
					i := 0
					for _, val := range test.data {
						if bytes.Equal(entry.key, val.key) {
							i++
							fmt.Printf("k: %v, v: %v\n", val.key, val.value)
						}
					}
					fmt.Printf("Number of times val exists: %d\n", i)
					fmt.Printf("Key added: %v\n", string(entry.key))
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

				lastRootHash, _ = tree.Root()
			}

			iterator := NewIterator(tree)
			count := 0
			for iterator.Next() {
				fmt.Printf("Key: %x , Val: %v\n", iterator.Key(), iterator.Value())
				count++
			}
			if count > 0 {
				fmt.Println(count)
				t.Fatal("Database is not empty")
			}
		})
	}
}

func TestTreeConsistencyStorage_PutGetDel(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"test1k-PutGetDel", CreateRandomValues(1000)},
		// {"test10k-PutGetDel", CreateRandomValues(10000)},
		// {"test100k-PutGetDel", CreateRandomValues(100000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tree := NewLevelTree(tmpDir)

			var lastRootHash []byte
			for _, entry := range test.data {
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
			err := tree.Close()
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree2 := NewLevelTree(tmpDir)

			lastRootHash = []byte{}
			stop := false
			testList := test.data
			var entry TestStruct

			for !stop {
				entry, testList = PickRandomKey(testList)
				if len(testList) == 0 {
					stop = true
				}

				val, err := tree2.Get(entry.key)

				if err != nil {
					t.Fatalf("value not found in the tree - %v - %v", entry.key, err)
				}
				if !bytes.Equal(val, entry.value) {
					t.Fatalf("unexpected value found in the tree - key: %v expected:  %v got: %v", entry.key, entry.value, val)
				}

				if tree2.Delete(entry.key) != nil {
					i := 0
					for _, val := range test.data {
						if bytes.Equal(entry.key, val.key) {
							i++
							fmt.Printf("k: %v, v: %v\n", val.key, val.value)
						}
					}
					fmt.Printf("Number of times val exists: %d\n", i)
					fmt.Printf("Key added: %v\n", string(entry.key))
					t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.key)
				}

				rootHash, err := tree2.Root()
				if err != nil {
					t.Fatalf("unable to fetch tree.Root() : %v", err)
				}

				if bytes.Equal(lastRootHash, rootHash) {
					fmt.Printf("Deleted key: %v\n", entry.key)
					fmt.Printf("lastRootHash: %v\n tree.Root: %v\n", lastRootHash, rootHash)
					t.Fatal("Root Hash didn't change after deletion")
				}

				lastRootHash, _ = tree2.Root()
			}
			err = tree2.Close()
			if err != nil {
				t.Fatal("Error closing the db")
			}

			tree3 := NewLevelTree(tmpDir)
			iterator := NewIterator(tree3)

			count := 0
			for iterator.Next() {
				fmt.Printf("Key: %x , Val: %v\n", iterator.Key(), iterator.Value())
				count++
			}
			if count > 0 {
				fmt.Println(count)
				t.Fatal("Database is not empty")
			}

			t.Cleanup(func() {
				time.Sleep(time.Second)
			})
		})
	}
}
