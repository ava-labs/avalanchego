// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/hashing"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const verificationCacheSize = 2_000

var (
	ErrInvalidProof                = errors.New("proof obtained an invalid root ID")
	ErrInvalidMaxLength            = errors.New("expected max length to be > 0")
	ErrNonIncreasingValues         = errors.New("keys sent are not in increasing order")
	ErrStateFromOutsideOfRange     = errors.New("state key falls outside of the start->end range")
	ErrNonIncreasingProofNodes     = errors.New("each proof node key must be a strict prefix of the next")
	ErrExtraProofNodes             = errors.New("extra proof nodes in path")
	ErrDataInMissingRootProof      = errors.New("there should be no state or deleted keys in a change proof that had a missing root")
	ErrNoMerkleProof               = errors.New("empty key response must include merkle proof")
	ErrShouldJustBeRoot            = errors.New("end proof should only contain root")
	ErrNoStartProof                = errors.New("no start proof")
	ErrNoEndProof                  = errors.New("no end proof")
	ErrNoProof                     = errors.New("proof has no nodes")
	ErrProofNodeNotForKey          = errors.New("the provided node has a key that is not a prefix of the specified key")
	ErrProofValueDoesntMatch       = errors.New("the provided value does not match the proof node for the provided key's value")
	ErrProofNodeHasUnincludedValue = errors.New("the provided proof has a value for a key within the range that is not present in the provided key/values")
	ErrInvalidMaybe                = errors.New("maybe is nothing but has value")
	ErrInvalidChildIndex           = fmt.Errorf("child index must be less than %d", NodeBranchFactor)
	ErrNilProofNode                = errors.New("proof node is nil")
	ErrNilValueOrHash              = errors.New("proof node's valueOrHash field is nil")
	ErrNilSerializedPath           = errors.New("serialized path is nil")
	ErrNilRangeProof               = errors.New("range proof is nil")
	ErrNilChangeProof              = errors.New("change proof is nil")
	ErrNilMaybeBytes               = errors.New("maybe bytes is nil")
)

type ProofNode struct {
	KeyPath SerializedPath
	// Nothing if this is an intermediate node.
	// The value in this node if its length < [HashLen].
	// The hash of the value in this node otherwise.
	ValueOrHash Maybe[[]byte]
	Children    map[byte]ids.ID
}

// Assumes [node.Key.KeyPath.NibbleLength] <= math.MaxUint64.
func (node *ProofNode) ToProto() *syncpb.ProofNode {
	pbNode := &syncpb.ProofNode{
		Key: &syncpb.SerializedPath{
			NibbleLength: uint64(node.KeyPath.NibbleLength),
			Value:        node.KeyPath.Value,
		},
		Children: make(map[uint32][]byte, len(node.Children)),
	}

	for childIndex, childID := range node.Children {
		childID := childID
		pbNode.Children[uint32(childIndex)] = childID[:]
	}

	if node.ValueOrHash.hasValue {
		pbNode.ValueOrHash = &syncpb.MaybeBytes{
			Value: node.ValueOrHash.value,
		}
	} else {
		pbNode.ValueOrHash = &syncpb.MaybeBytes{
			IsNothing: true,
		}
	}

	return pbNode
}

func (node *ProofNode) UnmarshalProto(pbNode *syncpb.ProofNode) error {
	switch {
	case pbNode == nil:
		return ErrNilProofNode
	case pbNode.ValueOrHash == nil:
		return ErrNilValueOrHash
	case pbNode.ValueOrHash.IsNothing && len(pbNode.ValueOrHash.Value) != 0:
		return ErrInvalidMaybe
	case pbNode.Key == nil:
		return ErrNilSerializedPath
	}

	node.KeyPath.NibbleLength = int(pbNode.Key.NibbleLength)
	node.KeyPath.Value = pbNode.Key.Value

	node.Children = make(map[byte]ids.ID, len(pbNode.Children))
	for childIndex, childIDBytes := range pbNode.Children {
		if childIndex >= NodeBranchFactor {
			return ErrInvalidChildIndex
		}
		childID, err := ids.ToID(childIDBytes)
		if err != nil {
			return err
		}
		node.Children[byte(childIndex)] = childID
	}

	if !pbNode.ValueOrHash.IsNothing {
		node.ValueOrHash = Some(pbNode.ValueOrHash.Value)
	}

	return nil
}

// An inclusion/exclustion proof of a key.
type Proof struct {
	// Nodes in the proof path from root --> target key
	// (or node that would be where key is if it doesn't exist).
	// Must always be non-empty (i.e. have the root node).
	Path []ProofNode
	// This is a proof that [key] exists/doesn't exist.
	Key []byte

	// Nothing if [Key] isn't in the trie.
	// Otherwise the value corresponding to [Key].
	Value Maybe[[]byte]
}

// Returns nil if the trie given in [proof] has root [expectedRootID].
// That is, this is a valid proof that [proof.Key] exists/doesn't exist
// in the trie with root [expectedRootID].
func (proof *Proof) Verify(ctx context.Context, expectedRootID ids.ID) error {
	// Make sure the proof is well-formed.
	if len(proof.Path) == 0 {
		return ErrNoProof
	}
	if err := verifyProofPath(proof.Path, newPath(proof.Key)); err != nil {
		return err
	}

	// Confirm that the last proof node's value matches the claimed proof value
	lastNode := proof.Path[len(proof.Path)-1]

	// If the last proof node's key is [proof.Key] (i.e. this is an inclusion proof)
	// then the value of the last proof node must match [proof.Value].
	// Note odd length keys can never match the [proof.Key] since it's bytes,
	// and thus an even number of nibbles.
	if !lastNode.KeyPath.hasOddLength() &&
		bytes.Equal(proof.Key, lastNode.KeyPath.Value) &&
		!valueOrHashMatches(proof.Value, lastNode.ValueOrHash) {
		return ErrProofValueDoesntMatch
	}

	// If the last proof node has an odd length or a different key than [proof.Key]
	// then this is an exclusion proof and should prove that [proof.Key] isn't in the trie..
	// Note odd length keys can never match the [proof.Key] since it's bytes,
	// and thus an even number of nibbles.
	if (lastNode.KeyPath.hasOddLength() || !bytes.Equal(proof.Key, lastNode.KeyPath.Value)) &&
		!proof.Value.IsNothing() {
		return ErrProofValueDoesntMatch
	}

	view, err := getEmptyTrieView(ctx)
	if err != nil {
		return err
	}

	// Insert all of the proof nodes.
	// [provenPath] is the path that we are proving exists, or the path
	// that is where the path we are proving doesn't exist should be.
	provenPath := proof.Path[len(proof.Path)-1].KeyPath.deserialize()

	// Don't bother locking [db] and [view] -- nobody else has a reference to them.
	if err = addPathInfo(view, proof.Path, provenPath, provenPath); err != nil {
		return err
	}

	gotRootID, err := view.GetMerkleRoot(ctx)
	if err != nil {
		return err
	}
	if expectedRootID != gotRootID {
		return fmt.Errorf("%w:[%s], expected:[%s]", ErrInvalidProof, gotRootID, expectedRootID)
	}
	return nil
}

type KeyValue struct {
	Key   []byte
	Value []byte
}

// A proof that a given set of key-value pairs are in a trie.
type RangeProof struct {
	// A proof that the smallest key in the requested range does/doesn't exist.
	// Note that this may not be an entire proof -- nodes are omitted if
	// they are also in [EndProof].
	StartProof []ProofNode

	// A proof of the greatest key in [KeyValues], or, if this proof contains
	// no [KeyValues], just the root.
	// Empty if the request for this range proof gave no upper bound
	// on the range to fetch, unless this proof contains no [KeyValues]
	// and [StartProof] is empty.
	EndProof []ProofNode

	// This proof proves that the key-value pairs in [KeyValues] are in the trie.
	// Sorted by increasing key.
	KeyValues []KeyValue
}

// Returns nil iff all the following hold:
//   - [start] <= [end].
//   - [proof] is non-empty.
//   - All keys in [proof.KeyValues] are in the range [start, end].
//     If [start] is empty, all keys are considered > [start].
//     If [end] is empty, all keys are considered < [end].
//   - [proof.KeyValues] is sorted by increasing key.
//   - [proof.StartProof] and [proof.EndProof] are well-formed.
//   - One of the following holds:
//     [end] and [proof.EndProof] are empty.
//     [proof.StartProof], [start], [end], and [proof.KeyValues] are empty and
//     [proof.EndProof] is just the root.
//     [end] is non-empty and [proof.EndProof] is a valid proof of a key <= [end].
//   - [expectedRootID] is the root of the trie containing the given key-value
//     pairs and start/end proofs.
func (proof *RangeProof) Verify(
	ctx context.Context,
	start []byte,
	end []byte,
	expectedRootID ids.ID,
) error {
	switch {
	case len(end) > 0 && bytes.Compare(start, end) > 0:
		return ErrStartAfterEnd
	case len(proof.KeyValues) == 0 && len(proof.StartProof) == 0 && len(proof.EndProof) == 0:
		return ErrNoMerkleProof
	case len(start) == 0 && len(end) == 0 && len(proof.KeyValues) == 0 && len(proof.EndProof) != 1:
		return ErrShouldJustBeRoot
	case len(proof.EndProof) == 0 && len(end) > 0:
		return ErrNoEndProof
	}

	// Make sure the key-value pairs are sorted and in [start, end].
	if err := verifyKeyValues(proof.KeyValues, start, end); err != nil {
		return err
	}

	largestkey := end
	if len(proof.KeyValues) > 0 {
		// If [proof] has key-value pairs, we should insert children
		// greater than [end] to ancestors of the node containing [end]
		// so that we get the expected root ID.
		largestkey = proof.KeyValues[len(proof.KeyValues)-1].Key
	}

	// The key-value pairs (allegedly) proven by [proof].
	keyValues := make(map[path][]byte, len(proof.KeyValues))
	for _, keyValue := range proof.KeyValues {
		keyValues[newPath(keyValue.Key)] = keyValue.Value
	}

	smallestPath := newPath(start)
	largestPath := newPath(largestkey)

	// Ensure that the start proof is valid and contains values that
	// match the key/values that were sent.
	if err := verifyProofPath(proof.StartProof, smallestPath); err != nil {
		return err
	}
	if err := verifyAllRangeProofKeyValuesPresent(proof.StartProof, smallestPath, largestPath, keyValues); err != nil {
		return err
	}

	// Ensure that the end proof is valid and contains values that
	// match the key/values that were sent.
	if err := verifyProofPath(proof.EndProof, largestPath); err != nil {
		return err
	}
	if err := verifyAllRangeProofKeyValuesPresent(proof.EndProof, smallestPath, largestPath, keyValues); err != nil {
		return err
	}

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := getEmptyTrieView(ctx)
	if err != nil {
		return err
	}

	// Insert all key-value pairs into the trie.
	for _, kv := range proof.KeyValues {
		if _, err := view.insertIntoTrie(newPath(kv.Key), Some(kv.Value)); err != nil {
			return err
		}
	}

	// For all the nodes along the edges of the proof, insert children < [start] and > [end]
	// into the trie so that we get the expected root ID (if this proof is valid).
	// By inserting all children < [start], we prove that there are no keys
	// > [start] but less than the first key given. That is, the peer who
	// gave us this proof is not omitting nodes.
	if err := addPathInfo(view, proof.StartProof, smallestPath, largestPath); err != nil {
		return err
	}
	if err := addPathInfo(view, proof.EndProof, smallestPath, largestPath); err != nil {
		return err
	}

	calculatedRoot, err := view.GetMerkleRoot(ctx)
	if err != nil {
		return err
	}
	if expectedRootID != calculatedRoot {
		return fmt.Errorf("%w:[%s], expected:[%s]", ErrInvalidProof, calculatedRoot, expectedRootID)
	}
	return nil
}

func (proof *RangeProof) ToProto() *syncpb.RangeProof {
	startProof := make([]*syncpb.ProofNode, len(proof.StartProof))
	for i, node := range proof.StartProof {
		startProof[i] = node.ToProto()
	}

	endProof := make([]*syncpb.ProofNode, len(proof.EndProof))
	for i, node := range proof.EndProof {
		endProof[i] = node.ToProto()
	}

	keyValues := make([]*syncpb.KeyValue, len(proof.KeyValues))
	for i, kv := range proof.KeyValues {
		keyValues[i] = &syncpb.KeyValue{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	return &syncpb.RangeProof{
		Start:     startProof,
		End:       endProof,
		KeyValues: keyValues,
	}
}

func (proof *RangeProof) UnmarshalProto(pbProof *syncpb.RangeProof) error {
	if pbProof == nil {
		return ErrNilRangeProof
	}

	proof.StartProof = make([]ProofNode, len(pbProof.Start))
	for i, protoNode := range pbProof.Start {
		if err := proof.StartProof[i].UnmarshalProto(protoNode); err != nil {
			return err
		}
	}

	proof.EndProof = make([]ProofNode, len(pbProof.End))
	for i, protoNode := range pbProof.End {
		if err := proof.EndProof[i].UnmarshalProto(protoNode); err != nil {
			return err
		}
	}

	proof.KeyValues = make([]KeyValue, len(pbProof.KeyValues))
	for i, kv := range pbProof.KeyValues {
		proof.KeyValues[i] = KeyValue{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	return nil
}

// Verify that all non-intermediate nodes in [proof] which have keys
// in [[start], [end]] have the value given for that key in [keysValues].
func verifyAllRangeProofKeyValuesPresent(proof []ProofNode, start, end path, keysValues map[path][]byte) error {
	for i := 0; i < len(proof); i++ {
		var (
			node     = proof[i]
			nodeKey  = node.KeyPath
			nodePath = nodeKey.deserialize()
		)

		// Skip odd length keys since they cannot have a value (enforced by [verifyProofPath]).
		if !nodeKey.hasOddLength() && nodePath.Compare(start) >= 0 && nodePath.Compare(end) <= 0 {
			value, ok := keysValues[nodePath]
			if !ok && !node.ValueOrHash.IsNothing() {
				// We didn't get a key-value pair for this key, but the proof node has a value.
				return ErrProofNodeHasUnincludedValue
			}
			if ok && !valueOrHashMatches(Some(value), node.ValueOrHash) {
				// We got a key-value pair for this key, but the value in the proof
				// node doesn't match the value we got for this key.
				return ErrProofValueDoesntMatch
			}
		}
	}
	return nil
}

type KeyChange struct {
	Key   []byte
	Value Maybe[[]byte]
}

type ChangeProof struct {
	// If false, the node that created this doesn't have
	// sufficient history to generate a change proof and
	// all other fields must be empty.
	// Otherwise at least one other field is non-empty.
	HadRootsInHistory bool
	// A proof that the smallest key in the requested range does/doesn't
	// exist in the trie with the requested start root.
	// Empty if no lower bound on the requested range was given.
	// Note that this may not be an entire proof -- nodes are omitted if
	// they are also in [EndProof].
	StartProof []ProofNode
	// A proof that the largest key in [KeyValues] and [DeletedKeys]
	// does/doesn't exist in the trie with the requested start root.
	// Empty iff no upper bound on the requested range was given
	// and [KeyValues] and [DeletedKeys] are empty.
	EndProof []ProofNode
	// A subset of key-values that were added, removed, or had their values modified
	// between the requested start root (exclusive) and the requested
	// end root (inclusive).
	// Sorted by increasing key.
	KeyChanges []KeyChange
}

func (proof *ChangeProof) ToProto() *syncpb.ChangeProof {
	startProof := make([]*syncpb.ProofNode, len(proof.StartProof))
	for i, node := range proof.StartProof {
		startProof[i] = node.ToProto()
	}

	endProof := make([]*syncpb.ProofNode, len(proof.EndProof))
	for i, node := range proof.EndProof {
		endProof[i] = node.ToProto()
	}

	keyChanges := make([]*syncpb.KeyChange, len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		var value syncpb.MaybeBytes
		if kv.Value.hasValue {
			value = syncpb.MaybeBytes{
				Value:     kv.Value.value,
				IsNothing: false,
			}
		} else {
			value = syncpb.MaybeBytes{
				Value:     nil,
				IsNothing: true,
			}
		}
		keyChanges[i] = &syncpb.KeyChange{
			Key:   kv.Key,
			Value: &value,
		}
	}

	return &syncpb.ChangeProof{
		HadRootsInHistory: proof.HadRootsInHistory,
		StartProof:        startProof,
		EndProof:          endProof,
		KeyChanges:        keyChanges,
	}
}

func (proof *ChangeProof) UnmarshalProto(pbProof *syncpb.ChangeProof) error {
	if pbProof == nil {
		return ErrNilChangeProof
	}

	proof.HadRootsInHistory = pbProof.HadRootsInHistory

	proof.StartProof = make([]ProofNode, len(pbProof.StartProof))
	for i, protoNode := range pbProof.StartProof {
		if err := proof.StartProof[i].UnmarshalProto(protoNode); err != nil {
			return err
		}
	}

	proof.EndProof = make([]ProofNode, len(pbProof.EndProof))
	for i, protoNode := range pbProof.EndProof {
		if err := proof.EndProof[i].UnmarshalProto(protoNode); err != nil {
			return err
		}
	}

	proof.KeyChanges = make([]KeyChange, len(pbProof.KeyChanges))
	for i, kv := range pbProof.KeyChanges {
		if kv.Value == nil {
			return ErrNilMaybeBytes
		}

		if kv.Value.IsNothing && len(kv.Value.Value) != 0 {
			return ErrInvalidMaybe
		}

		value := Nothing[[]byte]()
		if !kv.Value.IsNothing {
			value = Some(kv.Value.Value)
		}
		proof.KeyChanges[i] = KeyChange{
			Key:   kv.Key,
			Value: value,
		}
	}

	return nil
}

// Verifies that all values present in the [proof]:
// - Are nothing when deleted, not in the db, or the node has an odd path length.
// - if the node's path is within the key range, that has a value that matches the value passed in the change list or in the db
func verifyAllChangeProofKeyValuesPresent(
	ctx context.Context,
	db MerkleDB,
	proof []ProofNode,
	start path,
	end path,
	keysValues map[path]Maybe[[]byte],
) error {
	for i := 0; i < len(proof); i++ {
		var (
			node     = proof[i]
			nodeKey  = node.KeyPath
			nodePath = nodeKey.deserialize()
		)

		// Check the value of any node with a key that is within the range.
		// Skip odd length keys since they cannot have a value (enforced by [verifyProofPath]).
		if !nodeKey.hasOddLength() && nodePath.Compare(start) >= 0 && nodePath.Compare(end) <= 0 {
			value, ok := keysValues[nodePath]
			if !ok {
				// This value isn't in the list of key-value pairs we got.
				dbValue, err := db.GetValue(ctx, nodeKey.Value)
				if err != nil {
					if err != database.ErrNotFound {
						return err
					}
					// This key isn't in the database so proof node should have Nothing.
					value = Nothing[[]byte]()
				} else {
					// This key is in the database so proof node should have matching value.
					value = Some(dbValue)
				}
			}
			if !valueOrHashMatches(value, node.ValueOrHash) {
				return ErrProofValueDoesntMatch
			}
		}
	}
	return nil
}

func (proof *ChangeProof) Empty() bool {
	return len(proof.KeyChanges) == 0 &&
		len(proof.StartProof) == 0 && len(proof.EndProof) == 0
}

// Returns nil iff both hold:
// 1. [kvs] is sorted by key in increasing order.
// 2. All keys in [kvs] are in the range [start, end].
// If [start] is nil, there is no lower bound on acceptable keys.
// If [end] is nil, there is no upper bound on acceptable keys.
// If [kvs] is empty, returns nil.
func verifyKeyChanges(kvs []KeyChange, start, end []byte) error {
	if len(kvs) == 0 {
		return nil
	}

	// ensure that the keys are in increasing order
	for i := 0; i < len(kvs)-1; i++ {
		if bytes.Compare(kvs[i].Key, kvs[i+1].Key) >= 0 {
			return ErrNonIncreasingValues
		}
	}

	// ensure that the keys are within the range [start, end]
	if (len(start) > 0 && bytes.Compare(kvs[0].Key, start) < 0) ||
		(len(end) > 0 && bytes.Compare(kvs[len(kvs)-1].Key, end) > 0) {
		return ErrStateFromOutsideOfRange
	}

	return nil
}

// Returns nil iff both hold:
// 1. [kvs] is sorted by key in increasing order.
// 2. All keys in [kvs] are in the range [start, end].
// If [start] is nil, there is no lower bound on acceptable keys.
// If [end] is nil, there is no upper bound on acceptable keys.
// If [kvs] is empty, returns nil.
func verifyKeyValues(kvs []KeyValue, start, end []byte) error {
	hasLowerBound := len(start) > 0
	hasUpperBound := len(end) > 0
	for i := 0; i < len(kvs); i++ {
		if i < len(kvs)-1 && bytes.Compare(kvs[i].Key, kvs[i+1].Key) >= 0 {
			return ErrNonIncreasingValues
		}
		if (hasLowerBound && bytes.Compare(kvs[i].Key, start) < 0) ||
			(hasUpperBound && bytes.Compare(kvs[i].Key, end) > 0) {
			return ErrStateFromOutsideOfRange
		}
	}
	return nil
}

// Returns nil iff all the following hold:
//   - Any node with an odd nibble length, should not have a value associated with it
//     since all keys with values are written in bytes, so have even nibble length.
//   - Each key in [proof] is a strict prefix of the following key.
//   - Each key in [proof] is a strict prefix of [keyBytes], except possibly the last.
//   - If the last element in [proof] is [keyBytes], this is an inclusion proof.
//     Otherwise, this is an exclusion proof and [keyBytes] must not be in [proof].
func verifyProofPath(proof []ProofNode, keyPath path) error {
	provenKey := keyPath.Serialize()

	// loop over all but the last node since it will not have the prefix in exclusion proofs
	for i := 0; i < len(proof)-1; i++ {
		nodeKey := proof[i].KeyPath

		// intermediate nodes (nodes with odd nibble length) should never have a value associated with them
		if nodeKey.hasOddLength() && !proof[i].ValueOrHash.IsNothing() {
			return ErrOddLengthWithValue
		}

		// each node should have a key that has the proven key as a prefix
		if !provenKey.HasStrictPrefix(nodeKey) {
			return ErrProofNodeNotForKey
		}

		// each node should have a key that is a prefix of the next node's key
		nextKey := proof[i+1].KeyPath
		if !nextKey.HasStrictPrefix(nodeKey) {
			return ErrNonIncreasingProofNodes
		}
	}

	// check the last node for a value since the above loop doesn't check the last node
	if len(proof) > 0 {
		lastNode := proof[len(proof)-1]
		if lastNode.KeyPath.hasOddLength() && !lastNode.ValueOrHash.IsNothing() {
			return ErrOddLengthWithValue
		}
	}

	return nil
}

// Returns true if [value] and [valueDigest] match.
// [valueOrHash] should be the [ValueOrHash] field of a [ProofNode].
func valueOrHashMatches(value Maybe[[]byte], valueOrHash Maybe[[]byte]) bool {
	var (
		valueIsNothing  = value.IsNothing()
		digestIsNothing = valueOrHash.IsNothing()
	)

	switch {
	case valueIsNothing != digestIsNothing:
		// One is nothing and the other isn't -- no match.
		return false
	case valueIsNothing:
		// Both are nothing -- match.
		return true
	case len(value.value) < HashLength:
		return bytes.Equal(value.value, valueOrHash.value)
	default:
		valueHash := hashing.ComputeHash256(value.value)
		return bytes.Equal(valueHash, valueOrHash.value)
	}
}

// Adds each key/value pair in [proofPath] to [t].
// For each proof node, adds the children that are < [start] or > [end].
// If [start] is empty, no children are < [start].
// If [end] is empty, no children are > [end].
// Assumes [t.lock] is held.
func addPathInfo(
	t *trieView,
	proofPath []ProofNode,
	startPath path,
	endPath path,
) error {
	var (
		hasLowerBound = len(startPath) > 0
		hasUpperBound = len(endPath) > 0
	)

	for i := len(proofPath) - 1; i >= 0; i-- {
		proofNode := proofPath[i]
		keyPath := proofNode.KeyPath.deserialize()

		if len(keyPath)&1 == 1 && !proofNode.ValueOrHash.IsNothing() {
			// a value cannot have an odd number of nibbles in its key
			return ErrOddLengthWithValue
		}

		// load the node associated with the key or create a new one
		// pass nothing because we are going to overwrite the value digest below
		n, err := t.insertIntoTrie(keyPath, Nothing[[]byte]())
		if err != nil {
			return err
		}
		// We overwrite the valueDigest to be the hash provided in the proof
		// node because we may not know the pre-image of the valueDigest.
		n.valueDigest = proofNode.ValueOrHash

		if !hasLowerBound && !hasUpperBound {
			// No children of proof nodes are outside the range.
			// No need to add any children to [n].
			continue
		}

		// Add [proofNode]'s children which are outside the range [start, end].
		compressedPath := EmptyPath
		for index, childID := range proofNode.Children {
			if existingChild, ok := n.children[index]; ok {
				compressedPath = existingChild.compressedPath
			}
			childPath := keyPath.Append(index) + compressedPath
			if (hasLowerBound && childPath.Compare(startPath) < 0) ||
				(hasUpperBound && childPath.Compare(endPath) > 0) {
				n.addChildWithoutNode(index, compressedPath, childID)
			}
		}
	}

	return nil
}

func getEmptyTrieView(ctx context.Context) (*trieView, error) {
	tracer, err := trace.New(trace.Config{Enabled: false})
	if err != nil {
		return nil, err
	}
	db, err := newDatabase(
		ctx,
		memdb.New(),
		Config{
			Tracer:        tracer,
			NodeCacheSize: verificationCacheSize,
		},
		&mockMetrics{},
	)
	if err != nil {
		return nil, err
	}

	return db.newUntrackedView(defaultPreallocationSize)
}
