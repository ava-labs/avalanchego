package merkledb

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

func TestForestConsistencyCopy_PutGetDel(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"testConsistencyCopy-1k-CopyPutGetDel", CreateRandomValues(500)},
		// {"test10k-PutGetDel", CreateRandomValues(10000)},
		// {"test100k-PutGetDel", CreateRandomValues(100000)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			// Create base Tree - A staker tree, and add values to it
			stakerDir := t.TempDir()
			stakerForest := NewLevelForest(stakerDir)
			stakerTree, err := stakerForest.CreateEmptyTree(0)
			if err != nil {
				t.Fatalf("Error creating tree - %v", err)
			}

			// add values to the tree
			putAndTestRoot(t, stakerTree, test.data)
			// close it
			err = hardCloseDB(stakerTree)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			// open the staker tree and check if the values are correct
			stakerForest2 := NewLevelForest(stakerDir)
			stakerTree2, err := stakerForest2.CreateEmptyTree(0)
			if err == nil {
				t.Fatalf("Expected error creating an existing tree - %v", err)
			}

			stakerTree2, err = stakerForest2.Get(0)
			if err != nil {
				t.Fatalf("Error fetching the tree - %v", err)
			}

			getTest(t, stakerTree2, test.data)

			// create a bootstrap tree - populate values from the staker tree
			// populated by traversing the nodes instead of inserting the k/v pairs
			bootstrapDir := t.TempDir()
			bootstrapForest := NewLevelForest(bootstrapDir)

			bootstrapTree, err := bootstrapForest.CreateEmptyTree(0)
			if err != nil {
				t.Fatalf("Error creating tree - %v", err)
			}

			rootHash := genRootNodeID(0)

			node, err := stakerTree2.GetNode(rootHash)
			if err != nil {
				t.Fatalf("unable to fetch root Node - %v", err)
			}

			err = bootstrapTree.PutRootNode(node)
			if err != nil {
				t.Fatalf("unable to insert RootNode - %v", err)
			}

			var nodeStack [][]byte
			nodeStack = append(nodeStack, node.GetChildrenHashes()...)

			for len(nodeStack) != 0 {

				nodeHash := nodeStack[0]
				nodeStack = nodeStack[1:]

				node, err = stakerTree2.GetNode(nodeHash)
				if err != nil {
					t.Fatalf("unable to Node - %v", err)
				}

				err = bootstrapTree.PutNodeAndCheck(node, nodeHash)
				if err != nil {
					t.Fatalf("unable to insert Node - %v", err)
				}

				nodeStack = append(nodeStack, node.GetChildrenHashes()...)
			}

			getTest(t, bootstrapTree, test.data)
			err = hardCloseDB(bootstrapTree)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			err = hardCloseDB(stakerTree2)
			if err != nil {
				t.Fatal("Error closing the db")
			}
		})
	}
}

func TestForestConsistency_PutGetDel(t *testing.T) {
	testSize := 1000
	t0 := CreateRandomValues(testSize)
	t1 := CreateRandomValues(testSize)
	t2 := CreateRandomValues(testSize)

	tests := []ScenarioForestStruct{
		{
			Name: "Out Of Order Full Delete",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "PutGet Value on Tree 0",
						PutData: t0,
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "PutGet Value on Tree 1",
						PutData: t1,
					},
				},
				{
					Tree:         2,
					OldTree:      1,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "PutGet Value on Tree 2",
						PutData: t2,
					},
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name:    "Del t0 Values on Tree 2",
						DelData: t0,
					},
				},
				{
					Tree: 1,
					TreeScenario: ScenarioTestStruct{
						Name:    "Del t0 Values on Tree 1",
						DelData: t0,
					},
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name:    "Del t1 Values on Tree 2",
						DelData: t1,
					},
				},
				{
					Tree: 0,
					TreeScenario: ScenarioTestStruct{
						Name:    "Del t0 Values on Tree 0",
						DelData: t0,
					},
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name:    "Del t2 Values on Tree 2",
						DelData: t2,
					},
				},
				{
					Tree:         1,
					checkCleanDB: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "Del t1 Values on Tree 1",
						DelData: t1,
					},
				},
			},
		},
	}

	if false {
		// dumpTest(t, tests, "test.json")
		// tests = loadTest(t, "test.json")
		// tests[0].debugEntry = []byte{235, 6, 131, 166, 154, 73, 104, 230, 190, 102, 190, 15, 79, 36, 157, 15, 114, 81, 182, 35, 241, 55, 98, 66, 19, 97, 7, 31, 76, 98, 42}
	}

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForestConsistency_PutGetClear(t *testing.T) {
	testSize := 1000
	t0 := CreateRandomValues(testSize)
	t1 := CreateRandomValues(testSize)
	t2 := CreateRandomValues(testSize)

	tests := []ScenarioForestStruct{
		{
			Name: "Out Of Order Full Delete",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "PutGet Value on Tree 0",
						PutData: t0,
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "PutGet Value on Tree 1",
						PutData: t1,
					},
				},
				{
					Tree:         2,
					OldTree:      1,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name:    "PutGet Value on Tree 2",
						PutData: t2,
					},
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name:      "Del All Values on Tree 2",
						ClearTree: true,
					},
				},
				{
					Tree: 1,
					TreeScenario: ScenarioTestStruct{
						Name:      "Del All Values on Tree 1",
						ClearTree: true,
					},
				},
				{
					Tree:         0,
					checkCleanDB: true,
					TreeScenario: ScenarioTestStruct{
						Name:      "Del ALL Values on Tree 0",
						ClearTree: true,
					},
				},
			},
		},
	}

	if false {
		dumpTest(t, tests, "test.json")
		tests = loadTest(t, "test.json")
		writeTest(tests)
		// tests[0].debugEntry = []byte{125, 84, 98, 23}
		// tests[0].debugInfo = true
	}

	for _, test := range tests {
		testForest(t, test)
	}
}

//
//
// Utility Test Functions
//
//

func loadTest(t *testing.T, fileName string) []ScenarioForestStruct {
	var tests []ScenarioForestStruct
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		t.Fatal("Couldn't open file")
	}
	err = json.Unmarshal(data, &tests)
	if err != nil {
		t.Fatal("Couldn't unmarshal file")
	}
	return tests
}

func dumpTest(t *testing.T, tests []ScenarioForestStruct, fileName string) {
	f, err := os.Create(fileName)
	if err != nil {
		t.Fatal("Couldn't open file")
	}
	defer f.Close()

	file, _ := json.MarshalIndent(tests, "", " ")
	err = ioutil.WriteFile(fileName, file, 0600)
	if err != nil {
		t.Fatal("Couldn't open file")
	}
}

func writeTest(tests []ScenarioForestStruct) {
	for _, test := range tests {
		for _, forestTest := range test.ForestScenario {
			fmt.Printf("Tree: %v\n", forestTest.Tree)
			for _, kv := range forestTest.TreeScenario.PutData {
				fmt.Printf("\tkey: %v\n\tval: %v\n\n", BytesToKey(kv.Key).ToExpandedBytes(), kv.Value)
			}
		}
	}
}
