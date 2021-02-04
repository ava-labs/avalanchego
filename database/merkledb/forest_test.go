package merkledb

import (
	"bytes"
	"encoding/hex"
	"fmt"
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

type referenceCheck struct {
	key  string
	refs int32
}

type ScenarioTree struct {
	Tree         uint32
	OldTree      uint32
	IsDuplicate  bool
	ShouldCreate bool
	checkCleanDB bool
	printTree    bool
	TreeScenario ScenarioTestStruct
	refCheck     []referenceCheck
}

type ScenarioForestStruct struct {
	Name           string
	ForestScenario []ScenarioTree
	debugInfo      bool
	debugEntry     []byte
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
		err = t0.Put(data.Key, data.Value)
		if err != nil {
			t.Fatalf("not expected to fail inserting data - %v", err)
		}
	}

	t0Copy, err := f.Get(0)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}

	for _, data := range t0Values {
		nodeValue, err := t0Copy.Get(data.Key)
		if err != nil {
			t.Fatalf("not expected to fail getting value - %v", err)
		}

		if !bytes.Equal(nodeValue, data.Value) {
			t.Fatalf("not expected different values for the same key - %v", err)
		}
	}

	t11, err := f.Copy(0, 11)
	if err != nil {
		t.Fatalf("not expected to fail copying tree - %v", err)
	}

	for _, data := range CreateRandomValues(1) {
		err = t11.Put(data.Key, data.Value)
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
			Name: "TwoTreesOneLeaf",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					IsDuplicate:  false,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Insert Value on Tree 0",
						PutData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Change the Leaf Value",
						PutData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{2, 2, 2}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{2, 2, 2}},
						},
					},
				},
				{
					Tree:         0,
					ShouldCreate: false,
					TreeScenario: ScenarioTestStruct{
						Name:    "Check Leaf Value on Tree 0",
						PutData: []TestStruct{},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
						},
					},
				},
			},
		},
		{
			Name: "TwoTreesOneBranch",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					IsDuplicate:  false,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Insert Values on Tree 0",
						PutData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Change the Branch",
						PutData: []TestStruct{
							{Key: []byte{1, 1, 4}, Value: []byte{1, 1, 4}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
							{Key: []byte{1, 1, 4}, Value: []byte{1, 1, 4}},
						},
					},
				},
				{
					Tree:         0,
					ShouldCreate: false,
					TreeScenario: ScenarioTestStruct{
						Name:    "Check Leaf Value on Tree 0",
						PutData: []TestStruct{},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
						},
					},
				},
			},
		},
		{
			Name: "IncrementReferences",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Insert Values on Tree 0",
						PutData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					IsDuplicate:  true,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Change the Branch",
						PutData: []TestStruct{
							{Key: []byte{1, 1, 4}, Value: []byte{1, 1, 4}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
							{Key: []byte{1, 1, 4}, Value: []byte{1, 1, 4}},
						},
					},
				},
				{
					Tree: 1,
					TreeScenario: ScenarioTestStruct{
						Name: "IncrementRefs",
						DelData: []TestStruct{
							{Key: []byte{1, 1, 4}, Value: []byte{1, 1, 4}},
						},
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
						},
					},
				},
				{
					Tree: 0,
					TreeScenario: ScenarioTestStruct{
						Name: "Check Values on Tree 0",
						GetData: []TestStruct{
							{Key: []byte{1, 1, 1}, Value: []byte{1, 1, 1}},
							{Key: []byte{1, 1, 3}, Value: []byte{1, 1, 3}},
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
	debugInfo := false
	f := NewMemoryForest()
	t0, err := f.CreateEmptyTree(0)
	if err != nil {
		t.Fatalf("not expected to fail - %v", err)
	}
	count := 0
	t0Values := CreateRandomValues(100)
	for _, data := range t0Values {
		err = t0.Put(data.Key, data.Value)
		if err != nil {
			t.Fatalf("not expected to fail inserting data - %v", err)
		}

		if debugInfo {
			count++
			if count < 10 {
				fmt.Printf(
					"\n＊ \033[32m PUT Operation \033[0m - "+
						"\033[36m Scenario: %s \u001B[0m\n  ---> "+
						"OperationName: %s - \033[33m Tree:%d \u001B[0m \n",
					"TestForest_DeletedTree",
					"--",
					0)
				fmt.Printf("Inserting key: %v\n", BytesToKey(data.Key).ToExpandedBytes())
				t0.PrintTree()
			}
		}
	}

	err = f.Delete(0)
	if err != nil {
		t.Fatalf("not expected to fail deleting tree - %v", err)
	}
	for _, data := range t0Values {
		value, err := t0.Get(data.Key)
		if err == nil || bytes.Equal(value, data.Value) {
			t.Fatalf("shouldn't have been able to fetch the correct value")
		}
	}
}

func TestForest_Scenarios(t *testing.T) {
	tests := []ScenarioForestStruct{
		{
			Name: "1 Root Clean up",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Insert Data",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
					refCheck: []referenceCheck{
						{
							key:  "f258635ad5c7fcd4b7bd73d3e63c93d40d1644a71e7ec7a125eff85af38710e9",
							refs: 1,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 1,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 1,
						},
					},
				},
				{
					Tree:         0,
					checkCleanDB: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Delete Data",
						DelData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
				},
			},
		},
		{
			Name: "2 Roots 1 Shared: Change Value",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-Duplicate-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 5},
							},
						},
					},
				},
				{
					Tree: 0,
					refCheck: []referenceCheck{
						{
							key:  "f258635ad5c7fcd4b7bd73d3e63c93d40d1644a71e7ec7a125eff85af38710e9",
							refs: 1,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 2,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 1,
						},
					},
				},
				{
					Tree: 1,
					refCheck: []referenceCheck{
						{
							key:  "b203eac7f50d4238200e393f4533f7033ff0d7d99ce4f232d83ee25097d5dc2d",
							refs: 1,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 2,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "51e5f132c72e56444b126c12d4f611a5f57e9b134855741e3c7dce9cde95837e",
							refs: 1,
						},
					},
				},
			},
		},
		{
			Name: "2 Roots 1 Shared: New Value Insert",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-Duplicate-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{6, 6, 6},
								Value: []byte{6, 6, 6},
							},
						},
					},
					refCheck: []referenceCheck{
						{
							key:  "80ea3670b367e1b4aa83e0de85dff938922ed394a4d4a87587ccd303f9c00260",
							refs: 1,
						},
						{
							key:  "f258635ad5c7fcd4b7bd73d3e63c93d40d1644a71e7ec7a125eff85af38710e9",
							refs: 2,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 1,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 1,
						},
						{
							key:  "b2e6135405d7531b6c372c1ecbaba52048e55a7f4e9c942fff0b745893ca1a31",
							refs: 1,
						},
					},
				},
				{
					Tree: 0,
					refCheck: []referenceCheck{
						{
							key:  "f258635ad5c7fcd4b7bd73d3e63c93d40d1644a71e7ec7a125eff85af38710e9",
							refs: 2,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 1,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 1,
						},
					},
				},
				{
					Tree:         2,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root2-Duplicate-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 6, 6},
								Value: []byte{1, 6, 6},
							},
						},
					},
					refCheck: []referenceCheck{
						{
							key:  "0498ac2cc29620094c54631370d94b12cc6c2447908dc394bbf9ec5b5e3898a1",
							refs: 1,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 2,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 2,
						},
						{
							key:  "44254bddfec57c366814e057f6afc07f8dbeadf3b4f6ee891df69a56bafc5433",
							refs: 1,
						},
					},
				},
			},
		},
		{
			Name: "2 Roots 1 Shared: Delete",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-DuplicateRoot0-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 5},
							},
						},
					},
				},
				{
					Tree: 1,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-Duplicate-Delete",
						DelData: []TestStruct{
							{
								Key: []byte{1, 1, 1},
							},
						},
					},
					refCheck: []referenceCheck{
						{
							key:  "c001758dd347c11eec6bc27470482ffb6940cb5f2bc7287ceeafd381f3d03695",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 2,
						},
						{
							key:  "51e5f132c72e56444b126c12d4f611a5f57e9b134855741e3c7dce9cde95837e",
							refs: 1,
						},
					},
				},
				{
					Tree: 0,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-PrintValues",
					},
					refCheck: []referenceCheck{
						{
							key:  "f258635ad5c7fcd4b7bd73d3e63c93d40d1644a71e7ec7a125eff85af38710e9",
							refs: 1,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 1,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 2,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 1,
						},
					},
				},
				{
					Tree: 1,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-Duplicate-Delete-2",
						DelData: []TestStruct{
							{
								Key: []byte{1, 1, 2},
							},
						},
					},
					refCheck: []referenceCheck{
						{
							key:  "51e5f132c72e56444b126c12d4f611a5f57e9b134855741e3c7dce9cde95837e",
							refs: 1,
						},
					},
				},
				{
					Tree: 0,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-Full-Delete",
						DelData: []TestStruct{
							{
								Key: []byte{1, 1, 1},
							},
							{
								Key: []byte{1, 1, 2},
							},
							{
								Key: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					Tree:         1,
					checkCleanDB: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-Duplicate-Delete-3",
						DelData: []TestStruct{
							{
								Key: []byte{1, 3, 4},
							},
						},
					},
				},
			},
		},
		{
			Name: "3 Roots 2 Shared: Change Value",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
				},
				{
					Tree:         2,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name: "Root1-Duplicate-ChangeValue",
						PutData: []TestStruct{{
							Key:   []byte{1, 1, 2},
							Value: []byte{1, 2, 2},
						}},
					},
					refCheck: []referenceCheck{
						{
							key:  "b841ba33545215136edac6dbbea4ef8fd6ebd5a104e1c9884076ffb81e12c0e4",
							refs: 1,
						},
						{
							key:  "4ba6c2d37274bf10debf0759d81f1dca89706b2db55b9729971ea93551b98fc1",
							refs: 1,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 2,
						},
						{
							key:  "7d9f45b420e66daf44925ce002bfc249616c5e081c9a6a5eba051cc53748eb2b",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 2,
						},
					},
				},
				{
					Tree: 1,
					refCheck: []referenceCheck{
						{
							key:  "f258635ad5c7fcd4b7bd73d3e63c93d40d1644a71e7ec7a125eff85af38710e9",
							refs: 2,
						},
						{
							key:  "66c9e5c57f414e866d40fde40b476aa359d02e8b088fda7c969c7466586a92ff",
							refs: 1,
						},
						{
							key:  "955c54e73e3c8afd4be80ba3cc40f1d44406759fe4854ac699159f39a2ea54d4",
							refs: 2,
						},
						{
							key:  "34588e8d1d311f6bf7aacaef7d64160d757c1d63b57f3192d3adf56630878365",
							refs: 1,
						},
						{
							key:  "0b95e4556efaad77ba4cd7435ad99cecd9c7fde482663184dd117a5b10d73dca",
							refs: 2,
						},
					},
				},
			},
		},
		{
			Name: "3 Roots 2 Shared: New Value Insert",
			ForestScenario: []ScenarioTree{
				{
					Tree:         0,
					ShouldCreate: true,
					TreeScenario: ScenarioTestStruct{
						Name: "Root0-Insert",
						PutData: []TestStruct{
							{
								Key:   []byte{1, 1, 1},
								Value: []byte{1, 1, 1},
							},
							{
								Key:   []byte{1, 1, 2},
								Value: []byte{1, 1, 2},
							},
							{
								Key:   []byte{1, 3, 4},
								Value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					Tree:         1,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
				},
				{
					Tree:         2,
					OldTree:      0,
					ShouldCreate: true,
					IsDuplicate:  true,
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name: "Root2-Insert",
						PutData: []TestStruct{{
							Key:   []byte{6, 6, 6},
							Value: []byte{6, 6, 6},
						}},
					},
				},
				{
					Tree: 2,
					TreeScenario: ScenarioTestStruct{
						Name: "Root2-Delete",
						DelData: []TestStruct{{
							Key: []byte{1, 1, 1},
						}},
					},
				},
				{
					Tree: 1,
				},
				{
					Tree: 0,
				},
			},
		},
	}

	for _, test := range tests {
		testForest(t, test)
	}

}

func TestForest_Size_2_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{228, 186, 179, 248, 5, 146, 124, 90, 154, 63, 74, 120, 92, 79, 70, 46, 80, 165, 251, 63, 223, 79, 199, 113, 202, 209, 188, 247},
			Value: []byte{244, 112, 231, 230, 252, 234, 128, 73, 173, 223, 176, 197, 21, 247, 229, 198},
		},
		{
			Key:   []byte{56, 86, 168, 84, 142, 109, 247},
			Value: []byte{251, 139, 56, 153, 167, 155, 144, 58, 109, 148, 11, 141, 182, 86},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{173, 84, 174, 23, 243, 226},
			Value: []byte{60, 4, 161, 53, 38, 184, 231},
		},
		{
			Key:   []byte{7, 17, 1, 163, 96, 156, 192, 34, 209, 71, 116, 124, 11, 215, 236, 178, 88, 158, 80, 43, 126, 87, 60, 168, 18, 245, 107, 189, 249, 151},
			Value: []byte{187, 5, 152, 93, 60, 32, 65, 119, 65, 115, 20, 188, 104, 87, 227, 126, 98, 106, 54},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{18, 93, 215},
			Value: []byte{220, 54, 218, 199, 171, 246, 20, 250, 59, 64, 183, 240, 230, 76, 170, 189, 63, 162, 255, 130, 61, 251, 76, 23},
		},
		{
			Key:   []byte{127, 43, 227, 78, 175, 184, 59, 91, 141, 82, 17, 52, 39, 214, 52, 93, 35, 80},
			Value: []byte{241, 22, 173, 67, 149, 5, 30, 128, 120, 80, 54, 233, 174, 110, 44, 150, 77, 170, 205, 252, 234, 170, 221, 236, 254, 113, 104, 76, 243, 28},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_3_DependingBranches_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{93, 97, 28, 186, 59, 132, 171, 169, 210, 143, 235, 150, 143, 239, 162, 44, 105, 241, 173, 35, 50, 214, 4, 84, 230, 132, 0, 1},
			Value: []byte{163, 192, 14, 56, 193, 50, 126, 185, 83, 27, 93, 40, 251, 83, 65, 145},
		},
		{
			Key:   []byte{251, 170, 231, 198, 184, 229, 70},
			Value: []byte{9, 167, 230, 0, 214, 169, 9, 103, 52, 169, 53, 79, 24, 48},
		},
		{
			Key:   []byte{31, 224, 33, 51, 155, 109},
			Value: []byte{195, 126, 71, 20, 238, 153, 22},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{170, 175, 28, 148, 67, 111, 216, 74, 61, 225, 215, 205, 116, 201, 17, 97, 146, 142, 80, 44, 91, 73, 7, 109, 240, 218, 212, 90, 177, 143},
			Value: []byte{219, 164, 123, 120, 11, 180, 21, 39, 254, 101, 19, 5, 150, 54, 112, 5, 234, 82, 191},
		},
		{
			Key:   []byte{250, 72, 94},
			Value: []byte{72, 154, 38, 97, 139, 157, 196, 195, 76, 27, 92, 31, 105, 41, 27, 150, 15, 103, 15, 244, 155, 92, 130, 24},
		},
		{
			Key:   []byte{219, 71, 251, 96, 185, 201, 253, 164, 34, 103, 203, 38, 188, 45, 245, 210, 90, 154},
			Value: []byte{50, 211, 142, 129, 157, 96, 37, 96, 165, 213, 90, 126, 36, 10, 142, 106, 119, 91, 172, 132, 76, 25, 166, 87, 57, 222, 199, 144, 46, 50},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{116, 3, 24, 184, 5, 129, 19, 124, 170, 174, 211, 190, 115, 173, 118, 61, 96, 157, 158, 47, 111, 75, 226, 183, 118, 137, 122, 89, 167, 230},
			Value: []byte{206},
		},
		{
			Key:   []byte{144, 22, 193, 36, 70, 192, 83, 27, 158, 153, 69, 39, 174, 245, 249, 41, 84, 158, 60, 23, 129, 113, 224, 129},
			Value: []byte{165, 86, 5, 110, 221, 127, 119, 118, 236, 176, 110, 49, 17, 106, 238, 172, 231},
		},
		{
			Key:   []byte{151, 58, 220, 90, 129, 239, 68, 121, 215},
			Value: []byte{169, 167, 204, 48, 220, 238},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_3_IncreasingReferencedLeaves_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{156, 76, 59, 255, 68, 179, 234, 14, 61, 4, 156, 160, 157, 6, 140, 189, 97, 56, 199, 71, 46, 31, 107, 216, 252, 109, 145, 169},
			Value: []byte{153, 87, 151, 73, 116, 145, 88, 140, 215, 161, 4, 202, 225, 100, 152, 245},
		},
		{
			Key:   []byte{252, 111, 122, 79, 228, 112, 233},
			Value: []byte{190, 138, 125, 141, 243, 236, 191, 92, 42, 34, 210, 175, 172, 97},
		},
		{
			Key:   []byte{235, 254, 24, 63, 246, 50},
			Value: []byte{167, 1, 250, 110, 66, 152, 251},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{135, 21, 139, 184, 179, 48, 41, 122, 140, 48, 179, 234, 162, 5, 57, 105, 161, 144, 38, 128, 185, 182, 72, 243, 41, 92, 211, 240, 175, 185},
			Value: []byte{199, 89, 123, 205, 26, 195, 228, 105, 109, 199, 254, 87, 12, 197, 9, 231, 189, 187, 34},
		},
		{
			Key:   []byte{91, 196, 10},
			Value: []byte{118, 49, 213, 152, 32, 133, 243, 55, 108, 116, 168, 23, 186, 152, 215, 163, 39, 13, 10, 162, 141, 17, 79, 97},
		},
		{
			Key:   []byte{77, 40, 205, 97, 168, 35, 189, 192, 78, 116, 183, 149, 107, 36, 76, 231, 193, 93},
			Value: []byte{129, 171, 118, 107, 219, 253, 37, 188, 137, 101, 237, 51, 2, 192, 82, 199, 163, 214, 234, 98, 214, 151, 105, 153, 22, 118, 157, 82, 31, 205},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{72, 47, 102, 179, 161, 103, 36, 238, 255, 32, 114, 153, 103, 12, 65, 211, 35, 31, 115, 59, 137, 158, 104, 156, 135, 62, 244, 122, 47, 221},
			Value: []byte{1},
		},
		{
			Key:   []byte{41, 205, 95, 230, 11, 128, 94, 28, 207, 82, 60, 7, 149, 254, 24, 206, 207, 34, 70, 242, 95, 77, 148, 33},
			Value: []byte{192, 24, 141, 253, 14, 109, 52, 4, 236, 153, 6, 192, 207, 232, 34, 9, 240},
		},
		{
			Key:   []byte{168, 101, 151, 27, 87, 55, 5, 50, 99},
			Value: []byte{83, 123, 208, 135, 29, 121},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_3_BranchingOnDiffRoot_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{187, 93, 187, 24, 85, 248, 157, 254, 70, 213, 109, 202, 159, 184, 149, 158, 156, 155, 230, 30, 168, 168, 219, 242, 254, 82, 90, 144},
			Value: []byte{227, 184, 10, 189, 130, 250, 167, 48, 176, 137, 109, 192, 145, 131, 229, 114},
		},
		{
			Key:   []byte{70, 35, 34, 43, 164, 105, 14},
			Value: []byte{34, 21, 50, 105, 87, 20, 93, 53, 141, 14, 90, 171, 15, 5},
		},
		{
			Key:   []byte{220, 176, 182, 144, 234, 176},
			Value: []byte{142, 49, 37, 125, 245, 98, 0},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{1, 73, 136, 211, 212, 229, 21, 150, 255, 234, 185, 146, 235, 62, 85, 243, 187, 27, 213, 71, 56, 27, 205, 201, 170, 170, 219, 89, 178, 86},
			Value: []byte{233, 72, 50, 110, 57, 104, 202, 68, 86, 240, 28, 188, 184, 208, 139, 183, 195, 102, 114},
		},
		{
			Key:   []byte{169, 29, 21},
			Value: []byte{227, 215, 70, 36, 226, 230, 109, 136, 188, 205, 214, 59, 207, 118, 209, 96, 57, 114, 60, 122, 190, 7, 92, 47},
		},
		{
			Key:   []byte{132, 135, 203, 54, 195, 75, 245, 132, 70, 158, 232, 119, 147, 106, 161, 39, 37, 253},
			Value: []byte{132, 85, 171, 105, 85, 155, 112, 103, 109, 181, 175, 105, 9, 228, 239, 238, 101, 175, 179, 24, 159, 181, 58, 106, 52, 24, 214, 97, 162, 136},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{75, 46, 169, 31, 108, 64, 28, 70, 97, 190, 193, 167, 221, 192, 77, 8, 181, 190, 246, 6, 139, 88, 52, 99, 215, 204, 56, 156, 137, 98},
			Value: []byte{35},
		},
		{
			Key:   []byte{134, 76, 244, 98, 147, 113, 187, 175, 221, 158, 62, 191, 94, 151, 180, 254, 98, 245, 189, 125, 180, 139, 27, 196},
			Value: []byte{35, 143, 101, 120, 136, 252, 205, 178, 13, 7, 163, 119, 164, 21, 90, 14, 2},
		},
		{
			Key:   []byte{224, 173, 186, 122, 52, 176, 123, 137, 154},
			Value: []byte{179, 242, 25, 90, 155, 11},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_4_PassParentRefs_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{24, 106, 97, 138, 103, 131, 228, 32, 211, 247, 56, 86, 235, 105, 148, 215, 47, 41, 61, 24, 141, 234, 63, 39, 27, 35, 34, 201},
			Value: []byte{242, 25, 224, 207, 57, 11, 125, 146, 59, 136, 148, 240, 0, 156, 112, 97},
		},
		{
			Key:   []byte{44, 179, 68, 165, 160, 20, 249},
			Value: []byte{143, 166, 151, 225, 38, 147, 108, 37, 36, 181, 100, 254, 27, 185},
		},
		{
			Key:   []byte{233, 16, 176, 131, 252, 73},
			Value: []byte{204, 215, 223, 173, 240, 33, 176},
		},
		{
			Key:   []byte{224, 234, 27, 126, 225, 105, 211, 144, 156, 50, 40, 189, 53, 207, 141, 96, 15, 160, 200, 120, 203, 225, 108, 120, 222, 82, 89, 7, 91, 113},
			Value: []byte{76, 109, 31, 88, 193, 157, 182, 55, 169, 95, 145, 18, 161, 70, 152, 34, 42, 26, 172},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{107, 180, 153},
			Value: []byte{124, 32, 133, 145, 231, 23, 129, 72, 234, 90, 193, 5, 55, 149, 218, 71, 18, 89, 30, 200, 158, 152, 56, 104},
		},
		{
			Key:   []byte{231, 180, 100, 178, 72, 12, 63, 152, 93, 146, 122, 160, 205, 72, 110, 231, 151, 29},
			Value: []byte{129, 162, 118, 26, 9, 79, 156, 118, 198, 222, 73, 177, 246, 250, 255, 50, 58, 27, 83, 132, 91, 136, 10, 130, 16, 78, 158, 248, 197, 199},
		},
		{
			Key:   []byte{155, 108, 176, 245, 33, 31, 148, 35, 135, 68, 70, 186, 106, 252, 37, 171, 55, 116, 161, 124, 67, 183, 93, 0, 11, 241, 133, 117, 206, 81},
			Value: []byte{71},
		},
		{
			Key:   []byte{62, 236, 153, 73, 205, 33, 212, 18, 145, 115, 185, 125, 157, 14, 97, 225, 195, 42, 235, 130, 190, 41, 179, 134},
			Value: []byte{94, 167, 227, 232, 231, 170, 66, 188, 176, 196, 52, 65, 97, 52, 48, 58, 160},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{176, 156, 34, 89, 253, 195, 14, 250, 18},
			Value: []byte{60, 75, 201, 182, 190, 163},
		},
		{
			Key:   []byte{87, 193, 13},
			Value: []byte{45, 62, 176, 90, 148, 194},
		},
		{
			Key:   []byte{137, 163, 118, 163, 6, 5, 229, 49, 176, 251, 8, 188, 124, 105, 156, 118, 62, 84, 238, 230, 160, 11, 125, 140, 122, 106, 129},
			Value: []byte{192},
		},
		{
			Key:   []byte{218, 103, 22, 125, 4, 245, 80, 146, 46, 242, 210, 3, 122, 47, 254, 41, 245, 124, 92, 13, 103, 7, 249, 215, 219, 120, 84, 62, 4, 140},
			Value: []byte{112, 29, 11, 238, 190, 63, 9, 93, 221, 57, 177, 56, 241, 64, 149, 87, 195, 239, 40, 144, 27, 193, 110, 204, 158, 50},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_4_LeafNodePassRefs_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{68, 109, 71, 216, 116, 239, 41, 24, 8, 115, 1, 172, 115, 51, 8, 224, 113, 234, 129, 207, 254, 0, 196, 186, 47, 210, 131, 35},
			Value: []byte{28, 146, 170, 184, 66, 76, 23, 42, 221, 211, 109, 220, 82, 90, 66, 94},
		},
		{
			Key:   []byte{222, 163, 236, 111, 37, 48, 94},
			Value: []byte{35, 20, 20, 216, 200, 216, 162, 186, 50, 226, 124, 229, 222, 166},
		},
		{
			Key:   []byte{223, 65, 60, 63, 167, 68},
			Value: []byte{51, 40, 79, 75, 19, 132, 230},
		},
		{
			Key:   []byte{218, 200, 223, 125, 241, 164, 186, 137, 55, 1, 141, 170, 143, 152, 225, 168, 14, 150, 222, 238, 49, 118, 62, 55, 105, 103, 167, 129, 41, 149},
			Value: []byte{149, 166, 86, 153, 137, 145, 195, 18, 123, 91, 212, 39, 120, 39, 185, 247, 56, 67, 40},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{166, 92, 78},
			Value: []byte{143, 217, 87, 27, 143, 139, 161, 215, 155, 39, 246, 34, 42, 97, 69, 178, 123, 98, 213, 6, 236, 57, 58, 105},
		},
		{
			Key:   []byte{103, 206, 44, 239, 137, 13, 3, 181, 204, 216, 100, 114, 160, 207, 200, 170, 241, 252},
			Value: []byte{133, 210, 81, 205, 176, 165, 135, 89, 27, 255, 142, 47, 106, 14, 133, 240, 35, 85, 0, 20, 162, 229, 125, 195, 73, 57, 66, 36, 199, 134},
		},
		{
			Key:   []byte{223, 183, 24, 50, 24, 27, 6, 150, 163, 91, 239, 74, 6, 232, 105, 99, 115, 22, 174, 190, 128, 237, 56, 253, 29, 110, 23, 143, 204, 192},
			Value: []byte{26},
		},
		{
			Key:   []byte{76, 99, 39, 119, 172, 85, 191, 51, 179, 90, 108, 127, 203, 135, 228, 168, 225, 255, 44, 113, 92, 181, 49, 21},
			Value: []byte{192, 88, 144, 253, 151, 218, 123, 101, 254, 63, 176, 103, 210, 85, 196, 14, 48},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{132, 211, 128, 14, 150, 218, 250, 38, 255},
			Value: []byte{239, 114, 167, 128, 43, 76},
		},
		{
			Key:   []byte{95, 21, 161},
			Value: []byte{108, 243, 151, 191, 59, 24},
		},
		{
			Key:   []byte{4, 35, 101, 197, 254, 92, 21, 66, 106, 63, 179, 26, 242, 220, 178, 17, 23, 209, 22, 203, 11, 191, 157, 68, 121, 174, 34},
			Value: []byte{115},
		},
		{
			Key:   []byte{163, 12, 218, 119, 226, 139, 161, 7, 244, 86, 181, 130, 242, 11, 243, 231, 203, 54, 212, 253, 40, 224, 180, 2, 84, 216, 106, 161, 201, 165},
			Value: []byte{99, 169, 57, 255, 70, 112, 167, 109, 115, 150, 189, 218, 146, 221, 159, 233, 130, 205, 74, 171, 163, 7, 133, 84, 141, 31},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_4_CreateEqualBranches_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{34, 123, 221, 212, 174, 8, 45, 227, 21, 152, 162, 126, 162, 145, 163, 180, 82, 225, 104, 209, 21, 251, 204, 190, 122, 202, 227, 157},
			Value: []byte{12, 187, 25, 218, 250, 235, 164, 73, 50, 238, 211, 102, 177, 113, 247, 139},
		},
		{
			Key:   []byte{254, 117, 238, 87, 233, 111, 200},
			Value: []byte{51, 72, 213, 94, 180, 39, 35, 234, 62, 4, 32, 83, 183, 249},
		},
		{
			Key:   []byte{247, 45, 8, 58, 176, 254},
			Value: []byte{128, 144, 203, 39, 52, 191, 40},
		},
		{
			Key:   []byte{162, 110, 250, 77, 30, 241, 189, 21, 136, 174, 60, 78, 152, 8, 80, 184, 97, 91, 101, 202, 129, 2, 75, 220, 249, 139, 54, 174, 126, 90},
			Value: []byte{126, 122, 226, 130, 3, 80, 218, 181, 83, 126, 177, 241, 184, 36, 47, 24, 250, 174, 113},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{78, 45, 181},
			Value: []byte{134, 70, 45, 83, 94, 216, 197, 37, 254, 185, 52, 167, 112, 232, 84, 33, 41, 84, 3, 41, 24, 115, 234, 98},
		},
		{
			Key:   []byte{163, 179, 114, 86, 177, 77, 89, 31, 112, 225, 226, 50, 161, 57, 18, 135, 149, 16},
			Value: []byte{105, 18, 127, 199, 196, 222, 184, 76, 175, 88, 224, 168, 110, 234, 126, 113, 229, 147, 94, 76, 208, 112, 12, 28, 236, 145, 66, 203, 244, 30},
		},
		{
			Key:   []byte{84, 248, 208, 172, 207, 27, 147, 52, 60, 227, 241, 213, 92, 234, 161, 154, 186, 18, 208, 117, 151, 75, 38, 152, 213, 198, 61, 62, 79, 252},
			Value: []byte{21},
		},
		{
			Key:   []byte{160, 140, 225, 164, 8, 102, 77, 215, 58, 108, 113, 111, 5, 49, 144, 57, 123, 161, 247, 172, 70, 159, 226, 134},
			Value: []byte{12, 91, 76, 233, 59, 217, 250, 69, 94, 177, 46, 166, 96, 188, 233, 116, 39},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{181, 205, 179, 151, 240, 25, 157, 145, 57},
			Value: []byte{189, 3, 179, 222, 117, 77},
		},
		{
			Key:   []byte{197, 172, 143},
			Value: []byte{211, 41, 243, 240, 201, 97},
		},
		{
			Key:   []byte{125, 85, 216, 34, 204, 249, 132, 106, 183, 136, 160, 11, 245, 169, 223, 9, 21, 74, 246, 0, 18, 242, 16, 15, 22, 200, 90},
			Value: []byte{132},
		},
		{
			Key:   []byte{204, 212, 227, 83, 212, 50, 12, 251, 147, 31, 233, 249, 97, 94, 143, 49, 78, 77, 100, 196, 52, 195, 49, 87, 46, 81, 65, 88, 125, 157},
			Value: []byte{156, 185, 122, 229, 212, 218, 180, 189, 240, 173, 163, 157, 42, 245, 219, 53, 234, 38, 247, 155, 196, 248, 249, 178, 191, 181},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func TestForest_Size_4_MultipleLeaves_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			Key:   []byte{118, 172, 218, 248, 251, 186, 165, 85, 13, 201, 140, 139, 248, 23, 45, 27, 116, 69, 54, 160, 79, 187, 65, 40, 52, 195, 75, 185},
			Value: []byte{98, 174, 6, 194, 155, 218, 205, 231, 74, 74, 200, 162, 222, 42, 137, 144},
		},
		{
			Key:   []byte{123, 70, 168, 165, 1, 147, 94},
			Value: []byte{193, 157, 50, 113, 101, 70, 24, 232, 174, 187, 22, 161, 83, 33},
		},
		{
			Key:   []byte{65, 124, 28, 242, 193, 134},
			Value: []byte{217, 92, 67, 189, 50, 179, 204},
		},
		{
			Key:   []byte{226, 188, 136, 49, 155, 212, 215, 44, 122, 94, 247, 4, 196, 111, 206, 227, 123, 131, 74, 102, 227, 10, 145, 157, 82, 143, 34, 185, 244, 36},
			Value: []byte{5, 244, 137, 233, 23, 225, 172, 221, 149, 207, 195, 214, 90, 108, 85, 83, 198, 231, 153},
		},
	}
	t1 := []TestStruct{
		{
			Key:   []byte{158, 247, 162},
			Value: []byte{222, 232, 168, 140, 28, 122, 243, 23, 172, 136, 153, 141, 10, 221, 150, 106, 249, 117, 32, 1, 160, 177, 194, 197},
		},
		{
			Key:   []byte{87, 150, 81, 185, 157, 190, 14, 48, 112, 109, 254, 86, 246, 115, 233, 151, 39, 114},
			Value: []byte{169, 95, 175, 63, 28, 81, 230, 105, 129, 249, 12, 92, 175, 132, 231, 221, 247, 67, 60, 181, 84, 162, 152, 232, 196, 189, 146, 177, 249, 14},
		},
		{
			Key:   []byte{45, 94, 129, 246, 194, 161, 59, 19, 247, 202, 219, 246, 186, 227, 192, 158, 202, 135, 235, 134, 48, 89, 54, 232, 147, 181, 174, 222, 221, 253},
			Value: []byte{31},
		},
		{
			Key:   []byte{41, 56, 137, 114, 216, 100, 69, 72, 252, 246, 93, 143, 229, 199, 210, 31, 142, 172, 12, 220, 114, 235, 186, 21},
			Value: []byte{228, 176, 230, 57, 189, 103, 27, 44, 208, 130, 188, 28, 0, 35, 125, 62, 214},
		},
	}
	t2 := []TestStruct{
		{
			Key:   []byte{120, 232, 43, 40, 204, 226, 136, 112, 130},
			Value: []byte{119, 5, 79, 240, 51, 148},
		},
		{
			Key:   []byte{142, 185, 253},
			Value: []byte{191, 83, 193, 206, 143, 253},
		},
		{
			Key:   []byte{30, 31, 121, 159, 0, 56, 215, 236, 214, 178, 71, 130, 92, 199, 97, 230, 65, 245, 197, 29, 103, 59, 105, 212, 141, 56, 192},
			Value: []byte{27},
		},
		{
			Key:   []byte{30, 44, 188, 140, 164, 185, 7, 183, 153, 4, 105, 181, 59, 186, 141, 168, 152, 245, 79, 93, 224, 244, 141, 42, 249, 90, 237, 41, 173, 152},
			Value: []byte{192, 175, 61, 42, 168, 102, 163, 14, 54, 173, 197, 211, 40, 66, 63, 155, 126, 118, 212, 75, 16, 199, 137, 121, 84, 12},
		},
	}

	tests := []ScenarioForestStruct{
		{
			Name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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

	for _, test := range tests {
		testForest(t, test)
	}
}

func testForest(t *testing.T, test ScenarioForestStruct) {
	var err error
	forest := NewMemoryForest()

	t.Run(test.Name, func(t *testing.T) {
		for _, scenario := range test.ForestScenario {
			tree := testFetchTree(t, forest, scenario)

			if scenario.printTree || test.debugInfo {
				fmt.Printf("\n \033[35m PrintTree Before Operation \u001B[0m ＊Scenario: %s \n  ---> "+
					"\033[31m OperationName: %s \u001B[0m - Tree:%d\n", test.Name, scenario.TreeScenario.Name, scenario.Tree)
				tree.PrintTree()
			}

			for _, entry := range scenario.TreeScenario.PutData {
				if debugEntry(test.debugEntry, entry.Value) {
					fmt.Println()
				}
				err = tree.Put(entry.Key, entry.Value)
				if test.debugInfo || debugEntry(test.debugEntry, entry.Value) {
					fmt.Printf(
						"\n＊ \033[32m PUT Operation \033[0m - "+
							"\033[36m Scenario: %s \u001B[0m\n  ---> "+
							"OperationName: %s - \033[33m Tree:%d \u001B[0m \n",
						test.Name,
						scenario.TreeScenario.Name,
						scenario.Tree)
					fmt.Printf("Inserting key: %v\n", BytesToKey(entry.Key).ToExpandedBytes())
					tree.PrintTree()
				}
				if err != nil {
					t.Fatalf("unable to put %v - %v", entry, err)
				}
			}

			getData := scenario.TreeScenario.PutData
			if scenario.TreeScenario.GetData != nil {
				getData = scenario.TreeScenario.GetData
			}

			for _, entry := range getData {
				val, err := tree.Get(entry.Key)
				if err != nil {
					t.Fatalf("unable to fetched %v - %v", entry, err)
				}

				if !bytes.Equal(entry.Value, val) {
					t.Fatalf("fetched wrong val - expected: %v got: %v", entry.Value, val)
				}
			}

			for _, entry := range scenario.TreeScenario.DelData {
				if debugEntry(test.debugEntry, entry.Value) {
					fmt.Println()
				}
				err = tree.Delete(entry.Key)
				if test.debugInfo || debugEntry(test.debugEntry, entry.Value) {
					fmt.Printf(
						"\n＊ \033[32m Delete Operation \033[0m - "+
							"\033[36m Scenario: %s \u001B[0m\n  ---> "+
							"OperationName: %s - \033[33m Tree:%d \u001B[0m \n",
						test.Name,
						scenario.TreeScenario.Name,
						scenario.Tree)
					fmt.Printf("Deleting key: %v\n", BytesToKey(entry.Key).ToExpandedBytes())
					tree.PrintTree()
				}
				if err != nil {
					t.Fatalf("scenario: %s \n value not deleted in the tree as it was not found err: %v \nkey: %v"+
						"\nexpandedKey: %v", scenario.TreeScenario.Name, err, entry.Key, BytesToKey(entry.Key).ToExpandedBytes())
				}
			}

			if scenario.TreeScenario.ClearTree {
				err := tree.Clear()
				//err := forest.Delete(scenario.Tree)
				if err != nil {
					t.Fatalf("couldn't clear the Tree - %v", err)
				}
			}

			if scenario.printTree || test.debugInfo {
				fmt.Printf("\n \033[34m PrintTree After Operation \u001B[0m ＊Scenario: %s \n  ---> \033[31m OperationName: %s \u001B[0m - Tree:%d\n", test.Name, scenario.TreeScenario.Name, scenario.Tree)
				tree.PrintTree()
			}

			if scenario.checkCleanDB {
				checkDatabaseItems(t, tree)
			}

			for _, refCheck := range scenario.refCheck {
				testReferences(t, tree, refCheck, scenario.TreeScenario.Name)
			}
		}

	})
}

func testFetchTree(t *testing.T, f *Forest, scenario ScenarioTree) *Tree {
	if scenario.ShouldCreate {
		if scenario.IsDuplicate {
			tree, err := f.Copy(scenario.OldTree, scenario.Tree)
			if err != nil {
				t.Fatalf("unable to fetch the tree %v - %v", scenario.Tree, err)
			}
			return tree
		}
		tree, err := f.CreateEmptyTree(scenario.Tree)
		if err != nil {
			t.Fatalf("unable to fetch the tree %v - %v", scenario.Tree, err)
		}
		return tree
	}

	tree, err := f.Get(scenario.Tree)
	if err != nil {
		t.Fatalf("unable to fetch the tree %v - %v", scenario.Tree, err)
	}

	return tree
}

func testReferences(t *testing.T, tree *Tree, check referenceCheck, name string) {
	node := getNode(t, tree, check.key)
	if node.References(0) != check.refs {
		t.Fatalf("unexpected references - Test: %s - got: %d, expected: %d\n\t %v", name, node.References(0), check.refs, node)
	}

}

func getNode(t *testing.T, tree *Tree, hash string) Node {
	key, err := hex.DecodeString(hash)
	if err != nil {
		t.Fatal(err)
	}
	val, err := tree.persistence.GetNodeByHash(key)
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func debugEntry(entry []byte, value []byte) bool {
	if len(value) == 0 {
		return false
	}
	return bytes.Equal(entry, value)
}
