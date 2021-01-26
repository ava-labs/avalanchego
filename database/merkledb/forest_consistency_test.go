package merkledb

import "testing"

func TestForestConsistencyCopy_PutGetDel(t *testing.T) {

	tests := []struct {
		name string
		data []TestStruct
	}{
		{"testConsistencyCopy-1k-CopyPutGetDel", CreateRandomValues(1000)},
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
			err = HardCloseDB(stakerTree)
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
			err = HardCloseDB(bootstrapTree)
			if err != nil {
				t.Fatal("Error closing the db")
			}

			err = stakerTree2.Close()
			if err != nil {
				t.Fatal("Error closing the db")
			}
		})
	}
}
