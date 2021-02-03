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
	tree         uint32
	oldTree      uint32
	isDuplicate  bool
	shouldCreate bool
	checkCleanDB bool
	printTree    bool
	treeScenario ScenarioTestStruct
	refCheck     []referenceCheck
}

type ScenarioForestStruct struct {
	name           string
	forestScenario []ScenarioTree
	debugInfo      bool
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
		if err == nil || bytes.Equal(value, data.value) {
			t.Fatalf("shouldn't have been able to fetch the correct value")
		}
	}
}

func TestForest_PutGetDel(t *testing.T) {
	testSize := 1000
	t0 := CreateRandomValues(testSize)
	t1 := CreateRandomValues(testSize)
	t2 := CreateRandomValues(testSize)

	tests := []ScenarioForestStruct{
		{
			name: "Out Of Order Full Delete",
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
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 2",
						delData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 1",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 2",
						delData: t1,
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 0",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t2 Values on Tree 2",
						delData: t2,
					},
				},
				{
					tree:         1,
					checkCleanDB: true,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 1",
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

func TestForest_Size_2_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			key:   []byte{228, 186, 179, 248, 5, 146, 124, 90, 154, 63, 74, 120, 92, 79, 70, 46, 80, 165, 251, 63, 223, 79, 199, 113, 202, 209, 188, 247},
			value: []byte{244, 112, 231, 230, 252, 234, 128, 73, 173, 223, 176, 197, 21, 247, 229, 198},
		},
		{
			key:   []byte{56, 86, 168, 84, 142, 109, 247},
			value: []byte{251, 139, 56, 153, 167, 155, 144, 58, 109, 148, 11, 141, 182, 86},
		},
	}
	t1 := []TestStruct{
		{
			key:   []byte{173, 84, 174, 23, 243, 226},
			value: []byte{60, 4, 161, 53, 38, 184, 231},
		},
		{
			key:   []byte{7, 17, 1, 163, 96, 156, 192, 34, 209, 71, 116, 124, 11, 215, 236, 178, 88, 158, 80, 43, 126, 87, 60, 168, 18, 245, 107, 189, 249, 151},
			value: []byte{187, 5, 152, 93, 60, 32, 65, 119, 65, 115, 20, 188, 104, 87, 227, 126, 98, 106, 54},
		},
	}
	t2 := []TestStruct{
		{
			key:   []byte{18, 93, 215},
			value: []byte{220, 54, 218, 199, 171, 246, 20, 250, 59, 64, 183, 240, 230, 76, 170, 189, 63, 162, 255, 130, 61, 251, 76, 23},
		},
		{
			key:   []byte{127, 43, 227, 78, 175, 184, 59, 91, 141, 82, 17, 52, 39, 214, 52, 93, 35, 80},
			value: []byte{241, 22, 173, 67, 149, 5, 30, 128, 120, 80, 54, 233, 174, 110, 44, 150, 77, 170, 205, 252, 234, 170, 221, 236, 254, 113, 104, 76, 243, 28},
		},
	}

	tests := []ScenarioForestStruct{
		{
			name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 2",
						delData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 1",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 2",
						delData: t1,
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 0",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t2 Values on Tree 2",
						delData: t2,
					},
				},
				{
					tree:         1,
					checkCleanDB: true,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 1",
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

func TestForest_Size_3_DependingBranches_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			key:   []byte{93, 97, 28, 186, 59, 132, 171, 169, 210, 143, 235, 150, 143, 239, 162, 44, 105, 241, 173, 35, 50, 214, 4, 84, 230, 132, 0, 1},
			value: []byte{163, 192, 14, 56, 193, 50, 126, 185, 83, 27, 93, 40, 251, 83, 65, 145},
		},
		{
			key:   []byte{251, 170, 231, 198, 184, 229, 70},
			value: []byte{9, 167, 230, 0, 214, 169, 9, 103, 52, 169, 53, 79, 24, 48},
		},
		{
			key:   []byte{31, 224, 33, 51, 155, 109},
			value: []byte{195, 126, 71, 20, 238, 153, 22},
		},
	}
	t1 := []TestStruct{
		{
			key:   []byte{170, 175, 28, 148, 67, 111, 216, 74, 61, 225, 215, 205, 116, 201, 17, 97, 146, 142, 80, 44, 91, 73, 7, 109, 240, 218, 212, 90, 177, 143},
			value: []byte{219, 164, 123, 120, 11, 180, 21, 39, 254, 101, 19, 5, 150, 54, 112, 5, 234, 82, 191},
		},
		{
			key:   []byte{250, 72, 94},
			value: []byte{72, 154, 38, 97, 139, 157, 196, 195, 76, 27, 92, 31, 105, 41, 27, 150, 15, 103, 15, 244, 155, 92, 130, 24},
		},
		{
			key:   []byte{219, 71, 251, 96, 185, 201, 253, 164, 34, 103, 203, 38, 188, 45, 245, 210, 90, 154},
			value: []byte{50, 211, 142, 129, 157, 96, 37, 96, 165, 213, 90, 126, 36, 10, 142, 106, 119, 91, 172, 132, 76, 25, 166, 87, 57, 222, 199, 144, 46, 50},
		},
	}
	t2 := []TestStruct{
		{
			key:   []byte{116, 3, 24, 184, 5, 129, 19, 124, 170, 174, 211, 190, 115, 173, 118, 61, 96, 157, 158, 47, 111, 75, 226, 183, 118, 137, 122, 89, 167, 230},
			value: []byte{206},
		},
		{
			key:   []byte{144, 22, 193, 36, 70, 192, 83, 27, 158, 153, 69, 39, 174, 245, 249, 41, 84, 158, 60, 23, 129, 113, 224, 129},
			value: []byte{165, 86, 5, 110, 221, 127, 119, 118, 236, 176, 110, 49, 17, 106, 238, 172, 231},
		},
		{
			key:   []byte{151, 58, 220, 90, 129, 239, 68, 121, 215},
			value: []byte{169, 167, 204, 48, 220, 238},
		},
	}

	tests := []ScenarioForestStruct{
		{
			name: "PutDelGet Scenario 1 - Out Of Order Full Delete",
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
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 2",
						delData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 1",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 2",
						delData: t1,
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 0",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t2 Values on Tree 2",
						delData: t2,
					},
				},
				{
					tree:         1,
					checkCleanDB: true,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 1",
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

func TestForest_Size_3Again_PutGetDel(t *testing.T) {
	t0 := []TestStruct{
		{
			key:   []byte{156, 76, 59, 255, 68, 179, 234, 14, 61, 4, 156, 160, 157, 6, 140, 189, 97, 56, 199, 71, 46, 31, 107, 216, 252, 109, 145, 169},
			value: []byte{153, 87, 151, 73, 116, 145, 88, 140, 215, 161, 4, 202, 225, 100, 152, 245},
		},
		{
			key:   []byte{252, 111, 122, 79, 228, 112, 233},
			value: []byte{190, 138, 125, 141, 243, 236, 191, 92, 42, 34, 210, 175, 172, 97},
		},
		{
			key:   []byte{235, 254, 24, 63, 246, 50},
			value: []byte{167, 1, 250, 110, 66, 152, 251},
		},
	}
	t1 := []TestStruct{
		{
			key:   []byte{135, 21, 139, 184, 179, 48, 41, 122, 140, 48, 179, 234, 162, 5, 57, 105, 161, 144, 38, 128, 185, 182, 72, 243, 41, 92, 211, 240, 175, 185},
			value: []byte{199, 89, 123, 205, 26, 195, 228, 105, 109, 199, 254, 87, 12, 197, 9, 231, 189, 187, 34},
		},
		{
			key:   []byte{91, 196, 10},
			value: []byte{118, 49, 213, 152, 32, 133, 243, 55, 108, 116, 168, 23, 186, 152, 215, 163, 39, 13, 10, 162, 141, 17, 79, 97},
		},
		{
			key:   []byte{77, 40, 205, 97, 168, 35, 189, 192, 78, 116, 183, 149, 107, 36, 76, 231, 193, 93},
			value: []byte{129, 171, 118, 107, 219, 253, 37, 188, 137, 101, 237, 51, 2, 192, 82, 199, 163, 214, 234, 98, 214, 151, 105, 153, 22, 118, 157, 82, 31, 205},
		},
	}
	t2 := []TestStruct{
		{
			key:   []byte{72, 47, 102, 179, 161, 103, 36, 238, 255, 32, 114, 153, 103, 12, 65, 211, 35, 31, 115, 59, 137, 158, 104, 156, 135, 62, 244, 122, 47, 221},
			value: []byte{1},
		},
		{
			key:   []byte{41, 205, 95, 230, 11, 128, 94, 28, 207, 82, 60, 7, 149, 254, 24, 206, 207, 34, 70, 242, 95, 77, 148, 33},
			value: []byte{192, 24, 141, 253, 14, 109, 52, 4, 236, 153, 6, 192, 207, 232, 34, 9, 240},
		},
		{
			key:   []byte{168, 101, 151, 27, 87, 55, 5, 50, 99},
			value: []byte{83, 123, 208, 135, 29, 121},
		},
	}

	tests := []ScenarioForestStruct{
		{
			debugInfo: true,
			name:      "PutDelGet Scenario 1 - Out Of Order Full Delete",
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
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 2",
						delData: t0,
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 1",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 2",
						delData: t1,
					},
				},
				{
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name:    "Del t0 Values on Tree 0",
						delData: t0,
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name:    "Del t2 Values on Tree 2",
						delData: t2,
					},
				},
				{
					tree:         1,
					checkCleanDB: true,
					treeScenario: ScenarioTestStruct{
						name:    "Del t1 Values on Tree 1",
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

func TestForest_Scenarios(t *testing.T) {
	tests := []ScenarioForestStruct{
		{
			name: "1 Root Clean up",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Insert Data",
						putData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
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
					tree:         0,
					checkCleanDB: true,
					treeScenario: ScenarioTestStruct{
						name: "Delete Data",
						delData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
							},
						},
					},
				},
			},
		},
		{
			name: "2 Roots 1 Shared: Change Value",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Root0-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
					treeScenario: ScenarioTestStruct{
						name: "Root1-Duplicate-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 5},
							},
						},
					},
				},
				{
					tree: 0,
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
					tree: 1,
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
			name: "2 Roots 1 Shared: New Value Insert",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Root0-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
					treeScenario: ScenarioTestStruct{
						name: "Root1-Duplicate-Insert",
						putData: []TestStruct{
							{
								key:   []byte{6, 6, 6},
								value: []byte{6, 6, 6},
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
					tree: 0,
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
					tree:         2,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
					treeScenario: ScenarioTestStruct{
						name: "Root2-Duplicate-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 6, 6},
								value: []byte{1, 6, 6},
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
			name: "2 Roots 1 Shared: Delete",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Root0-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
					treeScenario: ScenarioTestStruct{
						name: "Root1-DuplicateRoot0-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 5},
							},
						},
					},
				},
				{
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name: "Root1-Duplicate-Delete",
						delData: []TestStruct{
							{
								key: []byte{1, 1, 1},
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
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name: "Root0-PrintValues",
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
					tree: 1,
					treeScenario: ScenarioTestStruct{
						name: "Root1-Duplicate-Delete-2",
						delData: []TestStruct{
							{
								key: []byte{1, 1, 2},
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
					tree: 0,
					treeScenario: ScenarioTestStruct{
						name: "Root0-Full-Delete",
						delData: []TestStruct{
							{
								key: []byte{1, 1, 1},
							},
							{
								key: []byte{1, 1, 2},
							},
							{
								key: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					tree:         1,
					checkCleanDB: true,
					treeScenario: ScenarioTestStruct{
						name: "Root1-Duplicate-Delete-3",
						delData: []TestStruct{
							{
								key: []byte{1, 3, 4},
							},
						},
					},
				},
			},
		},
		{
			name: "3 Roots 2 Shared: Change Value",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Root0-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
				},
				{
					tree:         2,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name: "Root1-Duplicate-ChangeValue",
						putData: []TestStruct{{
							key:   []byte{1, 1, 2},
							value: []byte{1, 2, 2},
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
					tree: 1,
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
			name: "3 Roots 2 Shared: New Value Insert",
			forestScenario: []ScenarioTree{
				{
					tree:         0,
					shouldCreate: true,
					treeScenario: ScenarioTestStruct{
						name: "Root0-Insert",
						putData: []TestStruct{
							{
								key:   []byte{1, 1, 1},
								value: []byte{1, 1, 1},
							},
							{
								key:   []byte{1, 1, 2},
								value: []byte{1, 1, 2},
							},
							{
								key:   []byte{1, 3, 4},
								value: []byte{1, 3, 4},
							},
						},
					},
				},
				{
					tree:         1,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
				},
				{
					tree:         2,
					oldTree:      0,
					shouldCreate: true,
					isDuplicate:  true,
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name: "Root2-Insert",
						putData: []TestStruct{{
							key:   []byte{6, 6, 6},
							value: []byte{6, 6, 6},
						}},
					},
				},
				{
					tree: 2,
					treeScenario: ScenarioTestStruct{
						name: "Root2-Delete",
						delData: []TestStruct{{
							key: []byte{1, 1, 1},
						}},
					},
				},
				{
					tree: 1,
				},
				{
					tree: 0,
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
			tree := testFetchTree(t, forest, &scenario)

			for _, entry := range scenario.treeScenario.putData {
				err = tree.Put(entry.key, entry.value)
				if test.debugInfo {
					fmt.Printf(
						"\n＊ \033[32m PUT Operation \033[0m - "+
							"\033[36m Scenario: %s \u001B[0m\n  ---> "+
							"OperationName: %s - \033[33m Tree:%d \u001B[0m \n",
						test.name,
						scenario.treeScenario.name,
						scenario.tree)
					fmt.Printf("Inserting key: %v\n", BytesToKey(entry.key).ToExpandedBytes())
					tree.PrintTree()
				}
				if err != nil {
					t.Fatalf("unable to put %v - %v", entry, err)
				}
			}
			//fmt.Println()
			//tree.PrintTree()

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
				if test.debugInfo {
					fmt.Printf(
						"\n＊ \033[32m Delete Operation \033[0m - "+
							"\033[36m Scenario: %s \u001B[0m\n  ---> "+
							"OperationName: %s - \033[33m Tree:%d \u001B[0m \n",
						test.name,
						scenario.treeScenario.name,
						scenario.tree)
					fmt.Printf("Deleting key: %v\n", BytesToKey(entry.key).ToExpandedBytes())
					tree.PrintTree()
				}
				if err != nil {
					t.Fatalf("scenario: %s \n value not deleted in the tree as it was not found err: %v \nkey: %v"+
						"\nexpandedKey: %v", scenario.treeScenario.name, err, entry.key, BytesToKey(entry.key).ToExpandedBytes())
				}
			}

			if scenario.printTree || test.debugInfo {
				fmt.Printf("\n \033[35m PrintTree Operation \u001B[0m ＊Scenario: %s \n  ---> \033[31m OperationName: %s \u001B[0m - Tree:%d\n", test.name, scenario.treeScenario.name, scenario.tree)
				tree.PrintTree()
			}

			if scenario.checkCleanDB {
				checkDatabaseItems(t, tree)
			}

			for _, refCheck := range scenario.refCheck {
				testReferences(t, tree, refCheck, scenario.treeScenario.name)
			}
		}

	})
}

func testFetchTree(t *testing.T, f *Forest, scenario *ScenarioTree) *Tree {
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
