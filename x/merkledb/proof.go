// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/maybe"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const verificationCacheSize = math.MaxUint16

var (
	ErrInvalidProof                  = errors.New("proof obtained an invalid root ID")
	ErrInvalidMaxLength              = errors.New("expected max length to be > 0")
	ErrNonIncreasingValues           = errors.New("keys sent are not in increasing order")
	ErrStateFromOutsideOfRange       = errors.New("state key falls outside of the start->end range")
	ErrNonIncreasingProofNodes       = errors.New("each proof node key must be a strict prefix of the next")
	ErrExtraProofNodes               = errors.New("extra proof nodes in path")
	ErrDataInMissingRootProof        = errors.New("there should be no state or deleted keys in a change proof that had a missing root")
	ErrEmptyProof                    = errors.New("proof is empty")
	ErrNoMerkleProof                 = errors.New("empty key response must include merkle proof")
	ErrShouldJustBeRoot              = errors.New("end proof should only contain root")
	ErrNoEndProof                    = errors.New("no end proof")
	ErrProofNodeNotForKey            = errors.New("the provided path has a key that is not a prefix of the specified key")
	ErrExclusionProofMissingEndNodes = errors.New("missing end nodes from path")
	ErrExclusionProofUnexpectedValue = errors.New("exclusion proof's value should be empty")
	ErrExclusionProofInvalidNode     = errors.New("invalid node for exclusion proof")
	ErrProofValueDoesntMatch         = errors.New("the provided value does not match the proof node for the provided key's value")
	ErrProofKeyPartialByte           = errors.New("the provided key has partial byte length")
	ErrProofNodeHasUnincludedValue   = errors.New("the provided proof has a value for a key within the range that is not present in the provided key/values")
	ErrInvalidMaybe                  = errors.New("maybe is nothing but has value")
	ErrNilProofNode                  = errors.New("proof node is nil")
	ErrNilValueOrHash                = errors.New("proof node's valueOrHash field is nil")
	ErrNilKey                        = errors.New("key is nil")
	ErrInvalidKeyLength              = errors.New("key length doesn't match bytes length, check specified branchFactor")
	ErrNilRangeProof                 = errors.New("range proof is nil")
	ErrNilChangeProof                = errors.New("change proof is nil")
	ErrNilMaybeBytes                 = errors.New("maybe bytes is nil")
	ErrNilProof                      = errors.New("proof is nil")
	ErrNilValue                      = errors.New("value is nil")
	ErrUnexpectedEndProof            = errors.New("end proof should be empty")
	ErrUnexpectedStartProof          = errors.New("start proof should be empty")
)

type ProofNode struct {
	Key Key
	// Nothing if this is an intermediate node.
	// The value in this node if its length < [HashLen].
	// The hash of the value in this node otherwise.
	ValueOrHash maybe.Maybe[[]byte]
	Children    map[byte]ids.ID
}

// ToProto converts the ProofNode into the protobuf version of a proof node
// Assumes [node.Key.Key.length] <= math.MaxUint64.
func (node *ProofNode) ToProto() *pb.ProofNode {
	pbNode := &pb.ProofNode{
		Key: &pb.Key{
			Length: uint64(node.Key.length),
			Value:  node.Key.Bytes(),
		},
		ValueOrHash: &pb.MaybeBytes{
			Value:     node.ValueOrHash.Value(),
			IsNothing: node.ValueOrHash.IsNothing(),
		},
		Children: make(map[uint32][]byte, len(node.Children)),
	}

	for childIndex, childID := range node.Children {
		pbNode.Children[uint32(childIndex)] = childID[:]
	}

	return pbNode
}

func (node *ProofNode) UnmarshalProto(pbNode *pb.ProofNode) error {
	switch {
	case pbNode == nil:
		return ErrNilProofNode
	case pbNode.ValueOrHash == nil:
		return ErrNilValueOrHash
	case pbNode.ValueOrHash.IsNothing && len(pbNode.ValueOrHash.Value) != 0:
		return ErrInvalidMaybe
	case pbNode.Key == nil:
		return ErrNilKey
	case len(pbNode.Key.Value) != bytesNeeded(int(pbNode.Key.Length)):
		return ErrInvalidKeyLength
	}
	node.Key = ToKey(pbNode.Key.Value).Take(int(pbNode.Key.Length))
	node.Children = make(map[byte]ids.ID, len(pbNode.Children))
	for childIndex, childIDBytes := range pbNode.Children {
		if childIndex > math.MaxUint8 {
			return errChildIndexTooLarge
		}
		childID, err := ids.ToID(childIDBytes)
		if err != nil {
			return err
		}
		node.Children[byte(childIndex)] = childID
	}

	if !pbNode.ValueOrHash.IsNothing {
		node.ValueOrHash = maybe.Some(pbNode.ValueOrHash.Value)
	}

	return nil
}

// Proof represents an inclusion/exclusion proof of a key.
type Proof struct {
	// Nodes in the proof path from root --> target key
	// (or node that would be where key is if it doesn't exist).
	// Always contains at least the root.
	Path []ProofNode
	// This is a proof that [key] exists/doesn't exist.
	// Must not have any partial bytes.
	Key Key

	// Nothing if [Key] isn't in the trie.
	// Otherwise, the value corresponding to [Key].
	Value maybe.Maybe[[]byte]
}

// Verify returns nil if the trie given in [proof] has root [expectedRootID].
// That is, this is a valid proof that [proof.Key] exists/doesn't exist
// in the trie with root [expectedRootID].
func (proof *Proof) Verify(
	ctx context.Context,
	expectedRootID ids.ID,
	tokenSize int,
	hasher Hasher,
) error {
	// Make sure the proof is well-formed.
	if len(proof.Path) == 0 {
		return ErrEmptyProof
	}

	if proof.Key.hasPartialByte() {
		return ErrProofKeyPartialByte
	}

	lastNode := proof.Path[len(proof.Path)-1]
	inclusionProof := lastNode.Key.Compare(proof.Key) == 0

	if inclusionProof && !valueOrHashMatches(hasher, proof.Value, lastNode.ValueOrHash) {
		return ErrProofValueDoesntMatch
	}

	if !inclusionProof && proof.Value.HasValue() {
		return ErrExclusionProofUnexpectedValue
	}

	if err := verifyProofPath(proof.Path, proof.Key, tokenSize); err != nil {
		return err
	}

	// Don't bother locking [view] -- nobody else has a reference to it.
	view, err := getStandaloneView(ctx, nil, tokenSize)
	if err != nil {
		return err
	}

	// Insert all proof nodes.
	// [provenKey] is the key that we are proving exists, or the key
	// that is the next key along the node path, proving that [proof.Key] doesn't exist in the trie.
	provenKey := maybe.Some(lastNode.Key)

	if err = addPathInfo(view, proof.Path, provenKey, provenKey); err != nil {
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

func (proof *Proof) ToProto() *pb.Proof {
	value := &pb.MaybeBytes{
		Value:     proof.Value.Value(),
		IsNothing: proof.Value.IsNothing(),
	}

	pbProof := &pb.Proof{
		Key:   proof.Key.Bytes(),
		Value: value,
	}

	pbProof.Proof = make([]*pb.ProofNode, len(proof.Path))
	for i, node := range proof.Path {
		pbProof.Proof[i] = node.ToProto()
	}

	return pbProof
}

func (proof *Proof) UnmarshalProto(pbProof *pb.Proof) error {
	switch {
	case pbProof == nil:
		return ErrNilProof
	case pbProof.Value == nil:
		return ErrNilValue
	case pbProof.Value.IsNothing && len(pbProof.Value.Value) != 0:
		return ErrInvalidMaybe
	}

	proof.Key = ToKey(pbProof.Key)

	if !pbProof.Value.IsNothing {
		proof.Value = maybe.Some(pbProof.Value.Value)
	}

	proof.Path = make([]ProofNode, len(pbProof.Proof))
	for i, pbNode := range pbProof.Proof {
		if err := proof.Path[i].UnmarshalProto(pbNode); err != nil {
			return err
		}
	}

	return nil
}

type KeyValue struct {
	Key   []byte
	Value []byte
}

// RangeProof is a proof that a given set of key-value pairs are in a trie.
type RangeProof struct {
	// Invariant: At least one of [StartProof], [EndProof], [KeyValues] is non-empty.

	// An inclusion/exclusion proof for the lower range bound.
	//
	// If no lower range bound was given, this is empty.
	//
	// Note that this may not be an entire proof -- nodes are omitted if
	// they are also in [EndProof].
	StartProof []ProofNode

	// An inclusion proof for the largest key in [KeyValues].
	//
	// If [KeyValues] is empty, this is an exclusion proof for the upper range bound.
	//
	// If [KeyValues] is empty, and no upper range bound was given, this is empty.
	EndProof []ProofNode

	// A subset of the requested key-value range, (because otherwise the proof may be too large)).
	// Each key is in the requested range (inclusive).
	// The first key-value is the first key-value at/after the range start.
	// The key-value pairs are consecutive.
	// Must not have any partial bytes.
	// Sorted by increasing key and with no duplicate keys.
	KeyValues []KeyValue
}

// Validate received data from change/range proof requests
// using the requested range.
func validateRangeChangeProof(
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	startProof []ProofNode,
	endProof []ProofNode,
	keys []Key,
	tokenSize int,
) error {
	switch {
	case start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) > 0:
		return ErrStartAfterEnd
	case len(keys) == 0 && len(startProof) == 0 && len(endProof) == 0:
		return ErrEmptyProof
	case end.IsNothing() && len(keys) == 0 && len(endProof) != 0:
		return ErrUnexpectedEndProof
	case start.IsNothing() && len(startProof) > 0:
		return ErrUnexpectedStartProof
	case len(endProof) == 0 && (end.HasValue() || len(keys) > 0):
		return ErrNoEndProof
	}

	var (
		// [startProof] is an inclusion/exclusion proof of [start]
		startProofKey = maybe.Bind(start, ToKey)

		// [endProof] is an inclusion of the largest key in [keyValues].
		//
		// If [keyValues] is empty:
		// - range proof: an exclusion proof of [end]
		// - change proof: an inclusion/exclusion proof of [end].
		endProofKey = maybe.Bind(end, ToKey)
	)

	// Make sure the key-value pairs are sorted and in [start, end].
	if err := verifyKeyValues(keys, startProofKey, endProofKey); err != nil {
		return err
	}

	if len(keys) > 0 {
		endProofKey = maybe.Some(keys[len(keys)-1])
	}

	// Ensure that the start proof is valid.
	// If [startProof] is non-empty, [end] is non-empty (length is checked inside verifyProofPath).
	if err := verifyProofPath(startProof, startProofKey.Value(), tokenSize); err != nil {
		return err
	}

	// Ensure that the end proof is valid.
	// If [endProof] is non-empty, [end] is non-empty (length is checked inside verifyProofPath).
	if err := verifyProofPath(endProof, endProofKey.Value(), tokenSize); err != nil {
		return err
	}

	return nil
}

// Verify returns nil iff all the following hold:
//   - The invariants of RangeProof hold.
//   - [start] <= [end].
//   - [proof] proves the key-value pairs in [proof.KeyValues] are in the trie
//     whose root is [expectedRootID].
//
// All keys in [proof.KeyValues] are in the range [start, end].
//
//	If [start] is Nothing, all keys are considered > [start].
//	If [end] is Nothing, all keys are considered < [end].
func (proof *RangeProof) Verify(
	ctx context.Context,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	expectedRootID ids.ID,
	tokenSize int,
	hasher Hasher,
) error {
	// [proof] allegedly provides and proves all key-value
	// pairs in [smallestProvenKey, largestProvenKey].
	// If [smallestProvenKey] is Nothing, [proof] should
	// provide and prove all keys < [largestProvenKey].
	// If [largestProvenKey] is Nothing, [proof] should
	// provide and prove all keys > [smallestProvenKey].
	// If both are Nothing, [proof] should prove the entire trie.

	// The key-value pairs (allegedly) proven by [proof].
	keyValues := make(map[Key][]byte, len(proof.KeyValues))
	keys := make([]Key, len(proof.KeyValues))
	for i, keyValue := range proof.KeyValues {
		k := ToKey(keyValue.Key)
		if k.hasPartialByte() {
			return ErrProofKeyPartialByte
		}

		keyValues[k] = keyValue.Value
		keys[i] = k
	}

	// Validate proof.
	err := validateRangeChangeProof(start, end, proof.StartProof, proof.EndProof, keys, tokenSize)
	if err != nil {
		return err
	}

	var (
		// [startProof] is an inclusion/exclusion proof of [startKey]
		startProofKey = maybe.Bind(start, ToKey)

		// [endProof] is an inclusion of the largest key in [keyValues],
		// or an exclusion proof of [end] if [keyValues] is empty.
		endProofKey = maybe.Bind(end, ToKey)
	)

	// Update [endProofKey] with the largest key in [keyValues].
	if len(proof.KeyValues) > 0 {
		// If [proof] has key-value pairs, we should insert children
		// greater than [largestProvenKey] to ancestors of the node containing
		// [largestProvenKey] so that we get the expected root ID.
		endProofKey = maybe.Some(ToKey(proof.KeyValues[len(proof.KeyValues)-1].Key))
	}

	// Ensure that the [startProof] contains values that match the key/values that were sent.
	if err := verifyAllRangeProofKeyValuesPresent(
		hasher,
		proof.StartProof,
		startProofKey,
		endProofKey,
		keyValues,
	); err != nil {
		return err
	}

	// Ensure that the [endProof] contains values that match the key/values that were sent.
	if err := verifyAllRangeProofKeyValuesPresent(
		hasher,
		proof.EndProof,
		startProofKey,
		endProofKey,
		keyValues,
	); err != nil {
		return err
	}

	// Insert all key-value pairs into the trie.
	ops := make([]database.BatchOp, len(proof.KeyValues))
	for i, kv := range proof.KeyValues {
		ops[i] = database.BatchOp{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	// Don't need to lock [view] because nobody else has a reference to it.
	view, err := getStandaloneView(ctx, ops, tokenSize)
	if err != nil {
		return err
	}

	// For all the nodes along the edges of the proof, insert children
	// < [smallestProvenKey] and > [largestProvenKey]
	// into the trie so that we get the expected root ID (if this proof is valid).
	// By inserting all children < [smallestProvenKey], we prove that there are no keys
	// > [smallestProvenKey] but less than the first key given.
	// That is, the peer who gave us this proof is not omitting nodes.
	if err := addPathInfo(
		view,
		proof.StartProof,
		startProofKey,
		endProofKey,
	); err != nil {
		return err
	}

	if err := addPathInfo(
		view,
		proof.EndProof,
		startProofKey,
		endProofKey,
	); err != nil {
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

func (proof *RangeProof) ToProto() *pb.RangeProof {
	startProof := make([]*pb.ProofNode, len(proof.StartProof))
	for i, node := range proof.StartProof {
		startProof[i] = node.ToProto()
	}

	endProof := make([]*pb.ProofNode, len(proof.EndProof))
	for i, node := range proof.EndProof {
		endProof[i] = node.ToProto()
	}

	keyValues := make([]*pb.KeyValue, len(proof.KeyValues))
	for i, kv := range proof.KeyValues {
		keyValues[i] = &pb.KeyValue{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	return &pb.RangeProof{
		StartProof: startProof,
		EndProof:   endProof,
		KeyValues:  keyValues,
	}
}

func (proof *RangeProof) UnmarshalProto(pbProof *pb.RangeProof) error {
	if pbProof == nil {
		return ErrNilRangeProof
	}

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
func verifyAllRangeProofKeyValuesPresent(
	hasher Hasher,
	proof []ProofNode,
	start maybe.Maybe[Key],
	end maybe.Maybe[Key],
	keysValues map[Key][]byte,
) error {
	for i := 0; i < len(proof); i++ {
		var (
			node    = proof[i]
			nodeKey = node.Key
		)

		// Skip keys that cannot have a value (enforced by [verifyProofPath]).
		if !nodeKey.hasPartialByte() && (start.IsNothing() || !nodeKey.Less(start.Value())) && (end.IsNothing() || !nodeKey.Greater(end.Value())) {
			value, ok := keysValues[nodeKey]
			if !ok && node.ValueOrHash.HasValue() {
				// We didn't get a key-value pair for this key, but the proof node has a value.
				return ErrProofNodeHasUnincludedValue
			}
			if ok && !valueOrHashMatches(hasher, maybe.Some(value), node.ValueOrHash) {
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
	Value maybe.Maybe[[]byte]
}

// ChangeProof proves that a set of key-value changes occurred
// between two trie roots, where each key-value pair's key is
// between some lower and upper bound (inclusive).
type ChangeProof struct {
	// Invariant: At least one of [StartProof], [EndProof], [KeyChanges] is non-empty.

	// An inclusion/exclusion proof for the lower range bound.
	//
	// If no lower range bound was given, this is empty.
	//
	// Note that this may not be an entire proof -- nodes are omitted if
	// they are also in [EndProof].
	StartProof []ProofNode

	// If [KeyChanges] is non-empty, this is a proof of the largest key
	// in [KeyChanges].
	//
	// If [KeyChanges] is empty and an upper range bound was given,
	// this is a proof of the upper range bound.
	//
	// If [KeyChanges] is empty and no upper range bound was given,
	// this is empty.
	EndProof []ProofNode

	// A subset of key-values that were added, removed, or had their values
	// modified between the requested start root (exclusive) and the requested
	// end root (inclusive).
	// Each key is in the requested range (inclusive).
	// The first key-value is the first key-value at/after the range start.
	// The key-value pairs are consecutive. That is, if keys k1 and k2 are
	// in [KeyChanges] then there is no k3 that was modified between the start and
	// end roots such that k1 < k3 < k2.
	// This is a subset of the requested key-value range, rather than the entire
	// range, because otherwise the proof may be too large.
	// Must not have any partial bytes.
	// Sorted by increasing key and with no duplicate keys.
	//
	// Example: Suppose that between the start root and the end root, the following
	// key-value pairs were added, removed, or modified:
	//
	// [kv1, kv2, kv3, kv4, kv5]
	// where start <= kv1 < ... < kv5 <= end.
	//
	// The following are possible values of [KeyChanges]:
	//
	// []
	// [kv1]
	// [kv1, kv2]
	// [kv1, kv2, kv3]
	// [kv1, kv2, kv3, kv4]
	// [kv1, kv2, kv3, kv4, kv5]
	//
	// The following values of [KeyChanges] are always invalid, for example:
	//
	// [kv2] (Doesn't include kv1, the first key-value at/after the range start)
	// [kv1, kv3] (Doesn't include kv2, the key-value between kv1 and kv3)
	// [kv1, kv3, kv2] (Not sorted by increasing key)
	// [kv1, kv1] (Duplicate key-value pairs)
	// [kv0, kv1] (For some kv1 < start)
	// [kv1, kv2, kv3, kv4, kv5, kv6] (For some kv6 > end)
	KeyChanges []KeyChange
}

func (proof *ChangeProof) ToProto() *pb.ChangeProof {
	startProof := make([]*pb.ProofNode, len(proof.StartProof))
	for i, node := range proof.StartProof {
		startProof[i] = node.ToProto()
	}

	endProof := make([]*pb.ProofNode, len(proof.EndProof))
	for i, node := range proof.EndProof {
		endProof[i] = node.ToProto()
	}

	keyChanges := make([]*pb.KeyChange, len(proof.KeyChanges))
	for i, kv := range proof.KeyChanges {
		keyChanges[i] = &pb.KeyChange{
			Key: kv.Key,
			Value: &pb.MaybeBytes{
				Value:     kv.Value.Value(),
				IsNothing: kv.Value.IsNothing(),
			},
		}
	}

	return &pb.ChangeProof{
		StartProof: startProof,
		EndProof:   endProof,
		KeyChanges: keyChanges,
	}
}

func (proof *ChangeProof) UnmarshalProto(pbProof *pb.ChangeProof) error {
	if pbProof == nil {
		return ErrNilChangeProof
	}

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

		value := maybe.Nothing[[]byte]()
		if !kv.Value.IsNothing {
			value = maybe.Some(kv.Value.Value)
		}
		proof.KeyChanges[i] = KeyChange{
			Key:   kv.Key,
			Value: value,
		}
	}

	return nil
}

// Verifies that all values present in the [proof]:
// - Are nothing when deleted, not in the db, or the node has key partial byte length
// - if the node's key is within the key range, that has a value that matches the value passed in the change list or in the db
func verifyAllChangeProofKeyValuesPresent(
	ctx context.Context,
	db *merkleDB,
	proof []ProofNode,
	start maybe.Maybe[Key],
	end maybe.Maybe[Key],
	keysValues map[Key]maybe.Maybe[[]byte],
) error {
	for i := 0; i < len(proof); i++ {
		var (
			node    = proof[i]
			nodeKey = node.Key
		)

		// Check the value of any node with a key that is within the range.
		// Skip keys that cannot have a value (enforced by [verifyProofPath]).
		if !nodeKey.hasPartialByte() && (start.IsNothing() || !nodeKey.Less(start.Value())) && (end.IsNothing() || !nodeKey.Greater(end.Value())) {
			value, ok := keysValues[nodeKey]
			if !ok {
				// This value isn't in the list of key-value pairs we got.
				dbValue, err := db.GetValue(ctx, nodeKey.Bytes())
				if err != nil {
					if !errors.Is(err, database.ErrNotFound) {
						return err
					}
					// This key isn't in the database so proof node should have Nothing.
					value = maybe.Nothing[[]byte]()
				} else {
					// This key is in the database so proof node should have matching value.
					value = maybe.Some(dbValue)
				}
			}
			if !valueOrHashMatches(db.hasher, value, node.ValueOrHash) {
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

// ChangeOrRangeProof has exactly one of [ChangeProof] or [RangeProof] is non-nil.
type ChangeOrRangeProof struct {
	ChangeProof *ChangeProof
	RangeProof  *RangeProof
}

// Returns nil iff both hold:
// 1. [kvs] is sorted by key in increasing order.
// 2. All keys in [kvs] are in the range [start, end].
// If [start] is nil, there is no lower bound on acceptable keys.
// If [end] is nothing, there is no upper bound on acceptable keys.
// If [kvs] is empty, returns nil.
func verifyKeyValues(kvs []Key, start maybe.Maybe[Key], end maybe.Maybe[Key]) error {
	hasLowerBound := start.HasValue()
	hasUpperBound := end.HasValue()
	for i := 0; i < len(kvs); i++ {
		if i < len(kvs)-1 && kvs[i].Compare(kvs[i+1]) >= 0 {
			return ErrNonIncreasingValues
		}

		if (hasLowerBound && kvs[i].Compare(start.Value()) < 0) ||
			(hasUpperBound && kvs[i].Compare(end.Value()) > 0) {
			return ErrStateFromOutsideOfRange
		}
	}
	return nil
}

// If the last element in [proof] is [key], this is an inclusion proof.
// Otherwise, this is an exclusion proof and [key] must not be in [proof].

// Returns nil iff all the following hold:
//
//   - Any node with a partial byte length, should not have a value associated with it
//     since all keys with values are written in complete bytes([]byte).
//
//   - Each key in [proof] is a strict prefix of the following key.
//
//   - Each key in [proof] is a strict prefix of [key], except possibly the last.
//
//   - If this is an inclusionProof, the last key in [proof] is the [key].
//
//   - If this is an exclusionProof:
//     -> the last key in [proof] is the replacement child and is at the corresponding index of the parent's children.
//     -> the last key in [proof] is the possible parent and it doesn't have a child at the corresponding index.
func verifyProofPath(proof []ProofNode, key Key, tokenSize int) error {
	if len(proof) == 0 {
		return nil
	}

	// loop over all but the last node since it will not have the prefix in exclusion proofs
	for i, proofNode := range proof[:len(proof)-1] {
		nodeKey := proofNode.Key

		// Because the interface only supports []byte keys,
		// a key with a partial byte may not store a value
		if nodeKey.hasPartialByte() && proofNode.ValueOrHash.HasValue() {
			return ErrPartialByteLengthWithValue
		}

		// each node's key should be a prefix of [key]
		if !key.HasStrictPrefix(nodeKey) {
			return ErrProofNodeNotForKey
		}

		// each node's key must be a prefix of the next node's key
		nextKey := proof[i+1].Key
		if !nextKey.HasStrictPrefix(nodeKey) {
			return ErrNonIncreasingProofNodes
		}
	}

	lastNode := proof[len(proof)-1]
	if lastNode.Key.hasPartialByte() && !lastNode.ValueOrHash.IsNothing() {
		return ErrPartialByteLengthWithValue
	}

	if lastNode.Key.Compare(key) != 0 {
		// exclusionProof

		if key.HasPrefix(lastNode.Key) {
			// [lastNode] is the parent of the node
			nextIndex := key.Token(lastNode.Key.length, tokenSize)

			if _, ok := lastNode.Children[nextIndex]; ok {
				// [lastNode] shouldn't contain any other child at the specific index
				return ErrExclusionProofMissingEndNodes
			}
		} else if len(proof) > 1 {
			// For [lastNode] to be the replacement child, it should be at the same index as [key] would be
			// inside the parent.
			// So, we need to check that the first [number of bits of the parent] + [tokenSize] bits of both,
			// [lastNode] and [key] are the same. Otherwise, it means the replacement child is at the wrong index.

			lastNodeParent := proof[len(proof)-2]
			parentKeyLen := lastNodeParent.Key.Length()
			bitsToCheck := parentKeyLen + tokenSize

			if !key.HasPrefix(lastNode.Key.Take(bitsToCheck)) {
				// [lastNode] at wrong index inside parent
				return ErrExclusionProofInvalidNode
			}
		}
	}

	return nil
}

// Returns true if [value] and [valueDigest] match.
// [valueOrHash] should be the [ValueOrHash] field of a [ProofNode].
func valueOrHashMatches(
	hasher Hasher,
	value maybe.Maybe[[]byte],
	valueOrHash maybe.Maybe[[]byte],
) bool {
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
	case len(value.Value()) < HashLength:
		return bytes.Equal(value.Value(), valueOrHash.Value())
	default:
		valueHash := hasher.HashValue(value.Value())
		return bytes.Equal(valueHash[:], valueOrHash.Value())
	}
}

// Adds each key/value pair in [proofPath] to [t].
// For each proof node, adds the children that are
// < [insertChildrenLessThan] or > [insertChildrenGreaterThan].
// If [insertChildrenLessThan] is Nothing, no children are < [insertChildrenLessThan].
// If [insertChildrenGreaterThan] is Nothing, no children are > [insertChildrenGreaterThan].
// Assumes [v.lock] is held.
func addPathInfo(
	v *view,
	proofPath []ProofNode,
	insertChildrenLessThan maybe.Maybe[Key],
	insertChildrenGreaterThan maybe.Maybe[Key],
) error {
	var (
		shouldInsertLeftChildren  = insertChildrenLessThan.HasValue()
		shouldInsertRightChildren = insertChildrenGreaterThan.HasValue()
	)

	for i := len(proofPath) - 1; i >= 0; i-- {
		proofNode := proofPath[i]
		key := proofNode.Key

		if key.hasPartialByte() && !proofNode.ValueOrHash.IsNothing() {
			return ErrPartialByteLengthWithValue
		}

		// load the node associated with the key or create a new one
		// pass nothing because we are going to overwrite the value digest below
		n, err := v.insert(key, maybe.Nothing[[]byte]())
		if err != nil {
			return err
		}
		// We overwrite the valueDigest to be the hash provided in the proof
		// node because we may not know the pre-image of the valueDigest.
		n.valueDigest = proofNode.ValueOrHash

		if !shouldInsertLeftChildren && !shouldInsertRightChildren {
			// No children of proof nodes are outside the range.
			// No need to add any children to [n].
			continue
		}

		// Add [proofNode]'s children which are outside the range
		// [insertChildrenLessThan, insertChildrenGreaterThan].
		// What is inside the range, should be included in provided key-values.
		for index, childID := range proofNode.Children {
			var compressedKey Key
			if existingChild, ok := n.children[index]; ok {
				compressedKey = existingChild.compressedKey
			}
			childKey := key.Extend(ToToken(index, v.tokenSize), compressedKey)
			if (shouldInsertLeftChildren && childKey.Less(insertChildrenLessThan.Value())) ||
				(shouldInsertRightChildren && childKey.Greater(insertChildrenGreaterThan.Value())) {
				// We don't set the [hasValue] field of the child but that's OK.
				// We only need the compressed key and ID to be correct so that the
				// calculated hash is correct.
				n.setChildEntry(
					index,
					&child{
						id:            childID,
						compressedKey: compressedKey,
					})
			}
		}
	}

	return nil
}

// getStandaloneView returns a new view that has nothing in it besides the changes due to [ops]
func getStandaloneView(ctx context.Context, ops []database.BatchOp, size int) (*view, error) {
	db, err := newDatabase(
		ctx,
		memdb.New(),
		Config{
			BranchFactor:                tokenSizeToBranchFactor[size],
			Tracer:                      trace.Noop,
			ValueNodeCacheSize:          verificationCacheSize,
			IntermediateNodeCacheSize:   verificationCacheSize,
			IntermediateWriteBufferSize: verificationCacheSize,
			IntermediateWriteBatchSize:  verificationCacheSize,
		},
		&mockMetrics{},
	)
	if err != nil {
		return nil, err
	}

	return newView(db, db, ViewChanges{BatchOps: ops, ConsumeBytes: true})
}
