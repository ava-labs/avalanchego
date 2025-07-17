// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ DBSyncClient = (*merkleDBSyncClient)(nil)

type DB interface {
	merkledb.Clearer
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}

type ClientConfig struct {
	Hasher       merkledb.Hasher
	BranchFactor merkledb.BranchFactor
}

type merkleDBSyncClient struct {
	db        DB
	config    *ClientConfig
	tokenSize int
	err       *utils.Atomic[error]
	errorOnce sync.Once
}

func NewClient(db DB, config *ClientConfig) (*merkleDBSyncClient, error) {
	if err := config.BranchFactor.Valid(); err != nil {
		return nil, err
	}

	if config.Hasher == nil {
		config.Hasher = merkledb.DefaultHasher
	}

	return &merkleDBSyncClient{
		db:        db,
		config:    config,
		tokenSize: merkledb.BranchFactorToTokenSize[config.BranchFactor],
		err:       utils.NewAtomic[error](nil),
	}, nil
}

func (c *merkleDBSyncClient) Error() error {
	return c.err.Get()
}

func (c *merkleDBSyncClient) setError(err error) {
	c.errorOnce.Do(func() {
		c.err.Set(err)
	})
}

func (c *merkleDBSyncClient) Clear() error {
	return c.db.Clear()
}

func (c *merkleDBSyncClient) GetRootHash(ctx context.Context) (ids.ID, error) {
	return c.db.GetMerkleRoot(ctx)
}

func (c *merkleDBSyncClient) HandleRangeProofResponse(ctx context.Context, request *pb.SyncGetRangeProofRequest, responseBytes []byte) (maybe.Maybe[[]byte], error) {
	var rangeProofProto pb.RangeProof
	if err := proto.Unmarshal(responseBytes, &rangeProofProto); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	var rangeProof merkledb.RangeProof
	if err := rangeProof.UnmarshalProto(&rangeProofProto); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	start := maybeBytesToMaybe(request.StartKey)
	end := maybeBytesToMaybe(request.EndKey)

	if err := verifyRangeProof(
		ctx,
		&rangeProof,
		int(request.KeyLimit),
		start,
		end,
		request.RootHash,
		c.tokenSize,
		c.config.Hasher,
	); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	largestHandledKey := end

	// Replace all the key-value pairs in the DB from start to end with values from the response.
	if err := c.db.CommitRangeProof(ctx, start, end, &rangeProof); err != nil {
		c.setError(err)
		return maybe.Nothing[[]byte](), err
	}

	if len(rangeProof.KeyChanges) > 0 {
		largestHandledKey = maybe.Some(rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key)
	}

	// Find the next key to fetch.
	// If this is empty, then we have no more keys to fetch.
	if !largestHandledKey.IsNothing() {
		nextKey, err := c.findNextKey(ctx, largestHandledKey.Value(), end, rangeProof.EndProof)
		if err != nil {
			c.setError(err)
			return maybe.Nothing[[]byte](), err
		}
		largestHandledKey = nextKey
	}

	return largestHandledKey, nil
}

func (c *merkleDBSyncClient) HandleChangeProofResponse(
	ctx context.Context,
	request *pb.SyncGetChangeProofRequest,
	responseBytes []byte,
) (maybe.Maybe[[]byte], error) {
	var changeProofResp pb.SyncGetChangeProofResponse
	if err := proto.Unmarshal(responseBytes, &changeProofResp); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	startKey := maybeBytesToMaybe(request.StartKey)
	endKey := maybeBytesToMaybe(request.EndKey)

	var (
		largestHandledKey maybe.Maybe[[]byte]
		endProof          []merkledb.ProofNode
	)
	switch changeProofResp := changeProofResp.Response.(type) {
	case *pb.SyncGetChangeProofResponse_ChangeProof:
		// The server had enough history to send us a change proof
		var changeProof merkledb.ChangeProof
		if err := changeProof.UnmarshalProto(changeProofResp.ChangeProof); err != nil {
			return maybe.Nothing[[]byte](), err
		}

		// Ensure the response does not contain more than the requested number of leaves
		// and the start and end roots match the requested roots.
		if len(changeProof.KeyChanges) > int(request.KeyLimit) {
			return maybe.Nothing[[]byte](), fmt.Errorf(
				"%w: (%d) > %d)",
				errTooManyKeys, len(changeProof.KeyChanges), request.KeyLimit,
			)
		}

		endRoot, err := ids.ToID(request.EndRootHash)
		if err != nil {
			return maybe.Nothing[[]byte](), err
		}

		if err := c.db.VerifyChangeProof(
			ctx,
			&changeProof,
			startKey,
			endKey,
			endRoot,
		); err != nil {
			return maybe.Nothing[[]byte](), fmt.Errorf("%w due to %w", errInvalidChangeProof, err)
		}

		largestHandledKey = endKey
		// if the proof wasn't empty, apply changes to the sync DB
		if len(changeProof.KeyChanges) > 0 {
			if err := c.db.CommitChangeProof(ctx, &changeProof); err != nil {
				c.setError(err)
				return maybe.Nothing[[]byte](), err
			}
			largestHandledKey = maybe.Some(changeProof.KeyChanges[len(changeProof.KeyChanges)-1].Key)
		}
		endProof = changeProof.EndProof

	case *pb.SyncGetChangeProofResponse_RangeProof:
		var rangeProof merkledb.RangeProof
		if err := rangeProof.UnmarshalProto(changeProofResp.RangeProof); err != nil {
			return maybe.Nothing[[]byte](), err
		}

		// The server did not have enough history to send us a change proof
		// so they sent a range proof instead.
		if err := verifyRangeProof(
			ctx,
			&rangeProof,
			int(request.KeyLimit),
			startKey,
			endKey,
			request.EndRootHash,
			c.tokenSize,
			c.config.Hasher,
		); err != nil {
			return maybe.Nothing[[]byte](), err
		}

		largestHandledKey = endKey
		if len(rangeProof.KeyChanges) > 0 {
			// Add all the key-value pairs we got to the database.
			if err := c.db.CommitRangeProof(ctx, startKey, endKey, &rangeProof); err != nil {
				c.setError(err)
				return maybe.Nothing[[]byte](), err
			}
			largestHandledKey = maybe.Some(rangeProof.KeyChanges[len(rangeProof.KeyChanges)-1].Key)
		}
		endProof = rangeProof.EndProof

	default:
		return maybe.Nothing[[]byte](), fmt.Errorf(
			"%w: %T",
			errUnexpectedChangeProofResponse, changeProofResp,
		)
	}
	// Find the next key to fetch.
	// If this is empty, then we have no more keys to fetch.
	if !largestHandledKey.IsNothing() {
		nextKey, err := c.findNextKey(ctx, largestHandledKey.Value(), endKey, endProof)
		if err != nil {
			c.setError(err)
			return maybe.Nothing[[]byte](), err
		}
		largestHandledKey = nextKey
	}

	return largestHandledKey, nil
}

// findNextKey returns the start of the key range that should be fetched next
// given that we just received a range/change proof that proved a range of
// key-value pairs ending at [lastReceivedKey].
//
// [rangeEnd] is the end of the range that we want to fetch.
//
// Returns Nothing if there are no more keys to fetch in [lastReceivedKey, rangeEnd].
//
// [endProof] is the end proof of the last proof received.
//
// Invariant: [lastReceivedKey] < [rangeEnd].
// If [rangeEnd] is Nothing it's considered > [lastReceivedKey].
func (c *merkleDBSyncClient) findNextKey(
	ctx context.Context,
	lastReceivedKey []byte,
	rangeEnd maybe.Maybe[[]byte],
	endProof []merkledb.ProofNode,
) (maybe.Maybe[[]byte], error) {
	if len(endProof) == 0 {
		// We try to find the next key to fetch by looking at the end proof.
		// If the end proof is empty, we have no information to use.
		// Start fetching from the next key after [lastReceivedKey].
		nextKey := lastReceivedKey
		nextKey = append(nextKey, 0)
		return maybe.Some(nextKey), nil
	}

	// We want the first key larger than the [lastReceivedKey].
	// This is done by taking two proofs for the same key
	// (one that was just received as part of a proof, and one from the local db)
	// and traversing them from the longest key to the shortest key.
	// For each node in these proofs, compare if the children of that node exist
	// or have the same ID in the other proof.
	proofKeyPath := merkledb.ToKey(lastReceivedKey)

	// If the received proof is an exclusion proof, the last node may be for a
	// key that is after the [lastReceivedKey].
	// If the last received node's key is after the [lastReceivedKey], it can
	// be removed to obtain a valid proof for a prefix of the [lastReceivedKey].
	if !proofKeyPath.HasPrefix(endProof[len(endProof)-1].Key) {
		endProof = endProof[:len(endProof)-1]
		// update the proofKeyPath to be for the prefix
		proofKeyPath = endProof[len(endProof)-1].Key
	}

	// get a proof for the same key as the received proof from the local db
	localProofOfKey, err := c.db.GetProof(ctx, proofKeyPath.Bytes())
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	localProofNodes := localProofOfKey.Path

	// The local proof may also be an exclusion proof with an extra node.
	// Remove this extra node if it exists to get a proof of the same key as the received proof
	if !proofKeyPath.HasPrefix(localProofNodes[len(localProofNodes)-1].Key) {
		localProofNodes = localProofNodes[:len(localProofNodes)-1]
	}

	nextKey := maybe.Nothing[[]byte]()

	// Add sentinel node back into the localProofNodes, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(localProofNodes) > 0 && localProofNodes[0].Key.Length() != 0 {
		sentinel := merkledb.ProofNode{
			Children: map[byte]ids.ID{
				localProofNodes[0].Key.Token(0, c.tokenSize): ids.Empty,
			},
		}
		localProofNodes = append([]merkledb.ProofNode{sentinel}, localProofNodes...)
	}

	// Add sentinel node back into the endProof, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(endProof) > 0 && endProof[0].Key.Length() != 0 {
		sentinel := merkledb.ProofNode{
			Children: map[byte]ids.ID{
				endProof[0].Key.Token(0, c.tokenSize): ids.Empty,
			},
		}
		endProof = append([]merkledb.ProofNode{sentinel}, endProof...)
	}

	localProofNodeIndex := len(localProofNodes) - 1
	receivedProofNodeIndex := len(endProof) - 1

	// traverse the two proofs from the deepest nodes up to the sentinel node until a difference is found
	for localProofNodeIndex >= 0 && receivedProofNodeIndex >= 0 && nextKey.IsNothing() {
		localProofNode := localProofNodes[localProofNodeIndex]
		receivedProofNode := endProof[receivedProofNodeIndex]

		// [deepestNode] is the proof node with the longest key (deepest in the trie) in the
		// two proofs that hasn't been handled yet.
		// [deepestNodeFromOtherProof] is the proof node from the other proof with
		// the same key/depth if it exists, nil otherwise.
		var deepestNode, deepestNodeFromOtherProof *merkledb.ProofNode

		// select the deepest proof node from the two proofs
		switch {
		case receivedProofNode.Key.Length() > localProofNode.Key.Length():
			// there was a branch node in the received proof that isn't in the local proof
			// see if the received proof node has children not present in the local proof
			deepestNode = &receivedProofNode

			// we have dealt with this received node, so move on to the next received node
			receivedProofNodeIndex--

		case localProofNode.Key.Length() > receivedProofNode.Key.Length():
			// there was a branch node in the local proof that isn't in the received proof
			// see if the local proof node has children not present in the received proof
			deepestNode = &localProofNode

			// we have dealt with this local node, so move on to the next local node
			localProofNodeIndex--

		default:
			// the two nodes are at the same depth
			// see if any of the children present in the local proof node are different
			// from the children in the received proof node
			deepestNode = &localProofNode
			deepestNodeFromOtherProof = &receivedProofNode

			// we have dealt with this local node and received node, so move on to the next nodes
			localProofNodeIndex--
			receivedProofNodeIndex--
		}

		// We only want to look at the children with keys greater than the proofKey.
		// The proof key has the deepest node's key as a prefix,
		// so only the next token of the proof key needs to be considered.

		// If the deepest node has the same key as [proofKeyPath],
		// then all of its children have keys greater than the proof key,
		// so we can start at the 0 token.
		startingChildToken := 0

		// If the deepest node has a key shorter than the key being proven,
		// we can look at the next token index of the proof key to determine which of that
		// node's children have keys larger than [proofKeyPath].
		// Any child with a token greater than the [proofKeyPath]'s token at that
		// index will have a larger key.
		if deepestNode.Key.Length() < proofKeyPath.Length() {
			startingChildToken = int(proofKeyPath.Token(deepestNode.Key.Length(), c.tokenSize)) + 1
		}

		// determine if there are any differences in the children for the deepest unhandled node of the two proofs
		if childIndex, hasDifference := findChildDifference(deepestNode, deepestNodeFromOtherProof, startingChildToken); hasDifference {
			nextKey = maybe.Some(deepestNode.Key.Extend(merkledb.ToToken(childIndex, c.tokenSize)).Bytes())
			break
		}
	}

	// If the nextKey is before or equal to the [lastReceivedKey]
	// then we couldn't find a better answer than the [lastReceivedKey].
	// Set the nextKey to [lastReceivedKey] + 0, which is the first key in
	// the open range (lastReceivedKey, rangeEnd).
	if nextKey.HasValue() && bytes.Compare(nextKey.Value(), lastReceivedKey) <= 0 {
		nextKeyVal := slices.Clone(lastReceivedKey)
		nextKeyVal = append(nextKeyVal, 0)
		nextKey = maybe.Some(nextKeyVal)
	}

	// If the [nextKey] is larger than the end of the range, return Nothing to signal that there is no next key in range
	if rangeEnd.HasValue() && bytes.Compare(nextKey.Value(), rangeEnd.Value()) >= 0 {
		return maybe.Nothing[[]byte](), nil
	}

	// the nextKey is within the open range (lastReceivedKey, rangeEnd), so return it
	return nextKey, nil
}

// findChildDifference returns the first child index that is different between node 1 and node 2 if one exists and
// a bool indicating if any difference was found
func findChildDifference(node1, node2 *merkledb.ProofNode, startIndex int) (byte, bool) {
	// Children indices >= [startIndex] present in at least one of the nodes.
	childIndices := set.Set[byte]{}
	for _, node := range []*merkledb.ProofNode{node1, node2} {
		if node == nil {
			continue
		}
		for key := range node.Children {
			if int(key) >= startIndex {
				childIndices.Add(key)
			}
		}
	}

	sortedChildIndices := maps.Keys(childIndices)
	slices.Sort(sortedChildIndices)
	var (
		child1, child2 ids.ID
		ok1, ok2       bool
	)
	for _, childIndex := range sortedChildIndices {
		if node1 != nil {
			child1, ok1 = node1.Children[childIndex]
		}
		if node2 != nil {
			child2, ok2 = node2.Children[childIndex]
		}
		// if one node has a child and the other doesn't or the children ids don't match,
		// return the current child index as the first difference
		if (ok1 || ok2) && child1 != child2 {
			return childIndex, true
		}
	}
	// there were no differences found
	return 0, false
}

// Verify [rangeProof] is a valid range proof for keys in [start, end] for
// root [rootBytes]. Returns [errTooManyKeys] if the response contains more
// than [keyLimit] keys.
func verifyRangeProof(
	ctx context.Context,
	rangeProof *merkledb.RangeProof,
	keyLimit int,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	rootBytes []byte,
	tokenSize int,
	hasher merkledb.Hasher,
) error {
	root, err := ids.ToID(rootBytes)
	if err != nil {
		return err
	}

	// Ensure the response does not contain more than the maximum requested number of leaves.
	if len(rangeProof.KeyChanges) > keyLimit {
		return fmt.Errorf(
			"%w: (%d) > %d)",
			errTooManyKeys, len(rangeProof.KeyChanges), keyLimit,
		)
	}

	if err := rangeProof.Verify(
		ctx,
		start,
		end,
		root,
		tokenSize,
		hasher,
	); err != nil {
		return fmt.Errorf("%w due to %w", errInvalidRangeProof, err)
	}
	return nil
}
