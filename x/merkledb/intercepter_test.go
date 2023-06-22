// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Intercepter(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	ctx := context.Background()
	emptyRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)

	viewIntf, err := db.NewView()
	require.NoError(err)

	view := viewIntf.(*trieView)
	require.NoError(view.Insert(ctx, []byte{0}, []byte{0, 1, 2}))

	newRoot, err := view.GetMerkleRoot(ctx)
	require.NoError(err)

	require.Len(view.touchedNodes, 1) // The root node (exclusion proof that []byte{0} existed before)

	require.FailNowf("done", "%s -> %s", emptyRoot, newRoot)
}

func Test_Intercepter_2(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	ctx := context.Background()
	emptyRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)

	viewIntf, err := db.NewView()
	require.NoError(err)

	view := viewIntf.(*trieView)
	require.NoError(view.Insert(ctx, []byte{0}, []byte{0, 1, 2}))
	require.NoError(view.Insert(ctx, []byte{1}, []byte{1, 2}))

	newRoot, err := view.GetMerkleRoot(ctx)
	require.NoError(err)

	require.Len(view.touchedNodes, 1) // The root node (exclusion proof that []byte{0} existed before)

	require.FailNowf("done", "%s -> %s", emptyRoot, newRoot)
}

func Test_Intercepter_3(t *testing.T) {
	require := require.New(t)

	db, err := getBasicDB()
	require.NoError(err)

	ctx := context.Background()
	emptyRoot, err := db.GetMerkleRoot(ctx)
	require.NoError(err)

	viewIntf1, err := db.NewView()
	require.NoError(err)

	view1 := viewIntf1.(*trieView)
	require.NoError(view1.Insert(ctx, []byte{0}, []byte{0, 1, 2}))
	require.NoError(view1.Insert(ctx, []byte{1}, []byte{1, 2}))

	expectedNewRoot, err := view1.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Len(view1.touchedNodes, 1) // The root node (exclusion proof that []byte{0} existed before)

	type proofNode struct {
		key  []byte
		node []byte
	}
	proof := make([]proofNode, 0, len(view1.touchedNodes))
	for _, node := range view1.touchedNodes {
		b, err := node.marshal()
		require.NoError(err)

		proof = append(proof, proofNode{
			key:  []byte(node.key),
			node: b,
		})
	}

	touchedNodesFromProof := make(map[path]*node)
	for _, node := range proof {
		key := path(node.key)
		n, err := parseNode(key, node.node)
		require.NoError(err)
		require.NoError(n.calculateID(view1.db.metrics))

		touchedNodesFromProof[key] = n
	}
	require.Len(touchedNodesFromProof, 1)
	require.Equal(view1.touchedNodes, touchedNodesFromProof)

	viewIntf2, err := db.NewView()
	require.NoError(err)

	view2 := viewIntf2.(*trieView)
	view2.mockedNodes = touchedNodesFromProof

	require.NoError(view2.Insert(ctx, []byte{0}, []byte{0, 1, 2}))
	require.NoError(view2.Insert(ctx, []byte{1}, []byte{1, 2}))

	newRoot, err := view2.GetMerkleRoot(ctx)
	require.NoError(err)
	require.Empty(view2.touchedNodes) // we shouldn't be touching any nodes anymore
	require.Equal(expectedNewRoot, newRoot)

	require.FailNowf("done", "%s -> %s", emptyRoot, expectedNewRoot)
}
