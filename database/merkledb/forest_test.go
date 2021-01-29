package merkledb

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/database/leveldb"

	"github.com/ava-labs/avalanchego/database/memdb"
)

func NewMemoryForest() *Forest {
	return NewForest(memdb.New())
}

func NewLevelForest(dir string) *Forest {
	db, err := leveldb.New(dir, 0, 0, 0)
	if err != nil {
		panic(err)
	}
	return NewForest(db)
}

type ScenarioTree struct {
	tree         uint32
	oldTree      uint32
	isDuplicate  bool
	shouldCreate bool
	treeScenario ScenarioTestStruct
}

type ScenarioForestStruct struct {
	name           string
	forestScenario []ScenarioTree
}

func TestForest_Basic(t *testing.T) {

	f := NewMemoryForest()
	_, err := f.Get(0)
	if err == nil {
		t.Fatal("no tree exists - expected to fail")
	}

	_, err = f.Copy(0, 0)
	if err == nil {
		t.Fatal("no tree exists - expected to fail")
	}

	err = f.Delete(0)
	if err == nil {
		t.Fatal("no tree exists - expected to fail")
	}

	t0, err := f.CreateEmptyTree(0)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}

	err = f.Delete(0)
	if err != nil {
		t.Fatalf("not expected to fail deleting an empty tree - %v", err)
	}

	t0, err = f.CreateEmptyTree(0)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}

	t0Values := CreateRandomValues(100)
	for _, data := range t0Values {
		err = t0.Put(data.key, data.value)
		if err != nil {
			t.Fatalf("not expected to fail inserting data - %v", err)
		}
	}

	t0Copy, err := f.Get(0)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}

	for _, data := range t0Values {
		nodeValue, err := t0Copy.Get(data.key)
		if err != nil {
			t.Fatalf("not expected to fail getting value - %v", err)
		}

		if !bytes.Equal(nodeValue, data.value) {
			t.Fatalf("not expected different values for the same key - %v", err)
		}
	}

	t11, err := f.Copy(0, 11)
	if err != nil {
		t.Fatalf("not expected to fail copying tree - %v", err)
	}

	for _, data := range CreateRandomValues(1) {
		err = t11.Put(data.key, data.value)
		if err != nil {
			t.Fatalf("not expected to fail inserting data - %v", err)
		}
	}

	err = f.Delete(0)
	if err != nil {
		t.Fatalf("not expected to fail deleting tree - %v", err)
	}

	err = f.Delete(11)
	if err != nil {
		t.Fatalf("not expected to fail deleting tree - %v", err)
	}

	t0, err = f.CreateEmptyTree(11)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}
}

func TestForest_Put(t *testing.T) {
	tests := []ScenarioForestStruct{
		{
			name: "TwoTreesOneLeaf",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					isDuplicate:  false,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Insert Value on Tree 0",
						putData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Change the Leaf Value",
						putData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{2, 2, 2}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{2, 2, 2}},
						},
					},
				},
				{
					tree:         0,
					shouldCreate: false,
					treeScenario: ScenarioTestStruct{
						name:    "Check Leaf Value on Tree 0",
						putData: []TestStruct{},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
						},
					},
				},
			},
		},
		{
			name: "TwoTreesOneBranch",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					isDuplicate:  false,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Insert Values on Tree 0",
						putData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Change the Branch",
						putData: []TestStruct{
							{key: []byte{1, 1, 4}, value: []byte{1, 1, 4}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
							{key: []byte{1, 1, 4}, value: []byte{1, 1, 4}},
						},
					},
				},
				{
					tree:         0,
					shouldCreate: false,
					treeScenario: ScenarioTestStruct{
						name:    "Check Leaf Value on Tree 0",
						putData: []TestStruct{},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
					},
				},
			},
		},
		{
			name: "IncrementReferences",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Insert Values on Tree 0",
						putData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Change the Branch",
						putData: []TestStruct{
							{key: []byte{1, 1, 4}, value: []byte{1, 1, 4}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
							{key: []byte{1, 1, 4}, value: []byte{1, 1, 4}},
						},
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name: "IncrementRefs",
						delData: []TestStruct{
							{key: []byte{1, 1, 4}, value: []byte{1, 1, 4}},
						},
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name: "Check Values on Tree 0",
						getData: []TestStruct{
							{key: []byte{1, 1, 1}, value: []byte{1, 1, 1}},
							{key: []byte{1, 1, 3}, value: []byte{1, 1, 3}},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_DeletedTree(t *testing.T) {
	f := NewMemoryForest()
	t0, err := f.CreateEmptyTree(0)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}
	t0Values := CreateRandomValues(100)
	for _, data := range t0Values {
		err = t0.Put(data.key, data.value)
		if err != nil {
			t.Fatalf("not expected to fail inserting data - %v", err)
		}
	}
	err = f.Delete(0)
	if err != nil {
		t.Fatalf("not expected to fail deleting tree - %v", err)
	}
	for _, data := range t0Values {
		value, err := t0.Get(data.key)
		if err == nil && bytes.Equal(value, data.value) {
			t.Fatalf("shouldn't have been able to fetch the correct value")
		}
	}
}

func TestForest_PutGetDel(t *testing.T) {
	testSize := 1000
	t0 := CreateRandomValues(testSize)
	t1 := CreateRandomValues(testSize)
	t2 := CreateRandomValues(testSize)
	t3 := CreateRandomValues(testSize)

	tests := []ScenarioForestStruct{
		{
			name: "OutOfOrderDelete",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 0",
						putData: t0,
					},
				},
				{
					tree:         1,
					oldTree:      0,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 1",
						putData: t1,
					},
				},
				{
					tree:         2,
					oldTree:      1,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 2",
						putData: t2,
					},
				},
				{
					tree:         3,
					oldTree:      2,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 3",
						putData: t3,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 2",
						delData: t2,
					},
				},
				{
					tree: 3,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 3",
						delData: t3,
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 0",
						delData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 1",
						delData: t1,
					},
				},
			},
		},
		{
			name: "OutOfOrderDelete",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 0",
						putData: t0,
					},
				},
				{
					tree:         1,
					oldTree:      0,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 1",
						putData: t1,
					},
				},
				{
					tree:         2,
					oldTree:      1,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 2",
						putData: t2,
					},
				},
				{
					tree:         3,
					oldTree:      2,
					isDuplicate:  true,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 3",
						putData: t3,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 2",
						delData: t2,
					},
				},
				{
					tree: 3,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 3",
						delData: t3,
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 0",
						delData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "Del Value on Tree 1",
						delData: t1,
					},
				},
			},
		},
	}

	for _, test := range tests {
		testForest(t, test)
	}
}

func testForest(t *testing.T, test ScenarioForestStruct) {
	var err error
	forest := NewMemoryForest()

	t.Run(test.name, func(t *testing.T) {
		for _, scenario := range test.forestScenario {
			tree := testFetchTree(t, forest, scenario)

			for _, entry := range scenario.treeScenario.putData {
				err = tree.Put(entry.key, entry.value)
				if err != nil {
					t.Fatalf("unable to put %v - %v", entry, err)
				}
				//tree.PrintTree()
				//fmt.Println()
			}

			getData := scenario.treeScenario.putData
			if scenario.treeScenario.getData != nil {
				getData = scenario.treeScenario.getData
			}

			for _, entry := range getData {
				val, err := tree.Get(entry.key)
				if err != nil {
					t.Fatalf("unable to fetched %v - %v", entry, err)
				}

				if !bytes.Equal(entry.value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.value, val)
				}
			}

			for _, entry := range scenario.treeScenario.delData {
				err = tree.Delete(entry.key)
				if err != nil {
					err = tree.Put(entry.key, entry.value)
					err = tree.Delete(entry.key)
					t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.key)
				}
			}
		}
	})
}

func testFetchTree(t *testing.T, f *Forest, scenario ScenarioTree) *Tree {
	if scenario.shouldCreate {
		if scenario.isDuplicate {
			tree, err := f.Copy(scenario.oldTree, scenario.tree)
			if err != nil {
				t.Fatalf("unable to fetch the tree %v - %v", scenario.tree, err)
			}
			return tree
		}
		tree, err := f.CreateEmptyTree(scenario.tree)
		if err != nil {
			t.Fatalf("unable to fetch the tree %v - %v", scenario.tree, err)
		}
		return tree
	}

	tree, err := f.Get(scenario.tree)
	if err != nil {
		t.Fatalf("unable to fetch the tree %v - %v", scenario.tree, err)
	}

	return tree
}
