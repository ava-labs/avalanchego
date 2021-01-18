package merkledb

import (
	"bytes"
	"testing"
)

type ScenarioTree struct {
	tree         int
	treeScenario ScenarioTestStruct
}

type ScenarioForestStruct struct {
	name           string
	forestScenario []ScenarioTree
}

func TestForest_Put(t *testing.T) {
	tests := []ScenarioForestStruct{
		{
			name: "TwoTreesOneLeaf",
			forestScenario: []ScenarioTree{
				{
					tree: 0,
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
					tree: 1,
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
					tree: 0,
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
					tree: 0,
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
					tree: 1,
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
					tree: 0,
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
	}

	for _, test := range tests {
		testForest(t, test)
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
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 0",
						putData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 1",
						putData: t1,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "PutGet Value on Tree 2",
						putData: t2,
					},
				},
				{
					tree: 3,
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
	tree := NewMemoryTree()
	t.Run(test.name, func(t *testing.T) {
		for _, scenario := range test.forestScenario {
			err = tree.SelectRoot(scenario.tree)
			if err != nil {
				t.Fatalf("unable to select root %v - %v", scenario.tree, err)
			}

			for _, entry := range scenario.treeScenario.putData {
				err = tree.Put(entry.key, entry.value)
				if err != nil {
					t.Fatalf("unable to put %v - %v", entry, err)
				}
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
				err := tree.Delete(entry.key)
				if err != nil {
					t.Fatalf("value not deleted in the tree as it was not found err: %v \nkey: %v", err, entry.key)
				}
			}
			// fmt.Printf("Tree: %v Root: %v\n\t↪Child: %v\n", scenario.tree, tree.persistence.rootNodes, tree.persistence.rootNodes[tree.persistence.currentRoot])
		}
	})
}
