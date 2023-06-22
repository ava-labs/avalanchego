// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func Test_Intercepter_empty_db(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	testIntercepter(require, db, []KeyChange{
		{
			Key:   []byte{0},
			Value: Some([]byte{0, 1, 2}),
		},
		{
			Key:   []byte{1},
			Value: Some([]byte{1, 2}),
		},
	})
}

func Test_Intercepter_non_empty_initial_db(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	require.NoError(db.Put([]byte{0}, []byte{2}))

	testIntercepter(require, db, []KeyChange{
		{
			Key:   []byte{0},
			Value: Some([]byte{0, 1, 2}),
		},
		{
			Key:   []byte{1},
			Value: Some([]byte{1, 2}),
		},
	})
}

type proofNode struct {
	key  []byte
	node []byte
}

func testIntercepter(require *require.Assertions, db *merkleDB, changes []KeyChange) {
	startRootID, startRoot, proof, endRootID := build(require, db, changes)
	verify(require, startRootID, startRoot, proof, changes, endRootID)
}

func build(
	require *require.Assertions,
	db *merkleDB,
	changes []KeyChange,
) (ids.ID, []byte, []proofNode, ids.ID) {
	ctx := context.Background()
	startRootID, err := db.GetMerkleRoot(ctx)
	require.NoError(err)

	startRootBytes, err := db.root.marshal()
	require.NoError(err)

	viewIntf1, err := db.NewView()
	require.NoError(err)

	var lock sync.Mutex
	touchedNodes := make(map[path]*node)
	view1 := viewIntf1.(*trieView)
	view1.enableIntercepter = true
	view1.touchedNodesLock = &lock
	view1.touchedNodes = touchedNodes

	viewIntf2, err := db.NewView()
	require.NoError(err)

	view2 := viewIntf2.(*trieView)
	view2.enableIntercepter = true
	view2.touchedNodesLock = &lock
	view2.touchedNodes = touchedNodes
	for _, change := range changes {
		if change.Value.IsNothing() {
			require.NoError(view2.Remove(ctx, change.Key))
		} else {
			require.NoError(view2.Insert(ctx, change.Key, change.Value.Value()))
		}
	}

	expectedNewRoot, err := view2.GetMerkleRoot(ctx)
	require.NoError(err)

	proof := make([]proofNode, 0, len(touchedNodes))
	for _, node := range touchedNodes {
		b, err := node.marshal()
		require.NoError(err)

		proof = append(proof, proofNode{
			key:  []byte(node.key),
			node: b,
		})
	}

	return startRootID, startRootBytes, proof, expectedNewRoot
}

func verify(
	require *require.Assertions,
	startRootID ids.ID,
	startRootBytes []byte,
	proof []proofNode,
	changes []KeyChange,
	expectedRootID ids.ID,
) {
	startRoot, err := parseNode(RootPath, startRootBytes)
	require.NoError(err)

	touchedNodesFromProof := make(map[path]*node)
	for _, node := range proof {
		key := path(node.key)
		n, err := parseNode(key, node.node)
		require.NoError(err)
		require.NoError(n.calculateID(&mockMetrics{}))

		touchedNodesFromProof[key] = n
	}

	parentView := &trieView{
		root: startRoot,
		db: &merkleDB{
			metrics: &mockMetrics{},
			tracer:  newNoopTracer(),
		},
		parentTrie:            nil,
		changes:               newChangeSummary(1),
		estimatedSize:         1,
		unappliedValueChanges: make(map[path]Maybe[[]byte], 1),

		enableIntercepter: true,
		mockedNodes:       touchedNodesFromProof,
		mockedMerkleRoot:  startRootID,
	}

	viewIntf, err := parentView.NewView()
	require.NoError(err)

	view := viewIntf.(*trieView)
	view.enableIntercepter = true
	view.mockedNodes = touchedNodesFromProof
	view.mockedMerkleRoot = startRootID

	ctx := context.Background()
	for _, change := range changes {
		if change.Value.IsNothing() {
			require.NoError(view.Remove(ctx, change.Key))
		} else {
			require.NoError(view.Insert(ctx, change.Key, change.Value.Value()))
		}
	}

	newRoot, err := view.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Equal(expectedRootID, newRoot)
}
