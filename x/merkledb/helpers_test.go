package merkledb

import (
	"bytes"
	"context"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func getBasicDB() (*merkleDB, error) {
	return newDatabase(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
		&mockMetrics{},
	)
}

// Writes []byte{i} -> []byte{i} for i in [0, 4]
func writeBasicBatch(t *testing.T, db *merkleDB) {
	require := require.New(t)

	batch := db.NewBatch()
	require.NoError(batch.Put([]byte{0}, []byte{0}))
	require.NoError(batch.Put([]byte{1}, []byte{1}))
	require.NoError(batch.Put([]byte{2}, []byte{2}))
	require.NoError(batch.Put([]byte{3}, []byte{3}))
	require.NoError(batch.Put([]byte{4}, []byte{4}))
	require.NoError(batch.Write())
}

func newRandomProofNode(r *rand.Rand) ProofNode {
	key := make([]byte, r.Intn(32)) // #nosec G404
	_, _ = r.Read(key)              // #nosec G404
	serializedKey := newPath(key).Serialize()

	val := make([]byte, r.Intn(64)) // #nosec G404
	_, _ = r.Read(val)              // #nosec G404

	children := map[byte]ids.ID{}
	for j := 0; j < NodeBranchFactor; j++ {
		if r.Float64() < 0.5 {
			var childID ids.ID
			_, _ = r.Read(childID[:]) // #nosec G404
			children[byte(j)] = childID
		}
	}

	hasValue := rand.Intn(2) == 1 // #nosec G404
	var valueOrHash maybe.Maybe[[]byte]
	if hasValue {
		// use the hash instead when length is greater than the hash length
		if len(val) >= HashLength {
			val = hashing.ComputeHash256(val)
		} else if len(val) == 0 {
			// We do this because when we encode a value of []byte{} we will later
			// decode it as nil.
			// Doing this prevents inconsistency when comparing the encoded and
			// decoded values.
			val = nil
		}
		valueOrHash = maybe.Some(val)
	}

	return ProofNode{
		KeyPath:     serializedKey,
		ValueOrHash: valueOrHash,
		Children:    children,
	}
}

func protoProofsEqual(proof1, proof2 *pb.Proof) bool {
	return (proof1 == nil && proof2 == nil) ||
		(proof1 != nil &&
			proof2 != nil &&
			protoProofNodesEqual(proof1.Proof, proof2.Proof) &&
			bytes.Equal(proof1.Key, proof2.Key) &&
			protoMaybeBytesEqual(proof1.Value, proof2.Value))
}

func protoProofNodesEqual(proof1, proof2 []*pb.ProofNode) bool {
	if len(proof1) != len(proof2) {
		return false
	}
	for i, node := range proof1 {
		if !protoProofNodeEqual(node, proof2[i]) {
			return false
		}
	}
	return true
}

func protoRangeProofsEqual(proof1, proof2 *pb.RangeProof) bool {
	return (proof1 == nil && proof2 == nil) ||
		(proof1 != nil &&
			proof2 != nil &&
			protoKeyValuesEqual(proof1.KeyValues, proof2.KeyValues) &&
			protoProofNodesEqual(proof1.Start, proof2.Start) &&
			protoProofNodesEqual(proof1.End, proof2.End))
}

func protoKeyValuesEqual(kv1, kv2 []*pb.KeyValue) bool {
	if len(kv1) != len(kv2) {
		return false
	}
	for i, kv := range kv1 {
		if !protoKeyValueEqual(kv, kv2[i]) {
			return false
		}
	}
	return true
}

func protoChangeProofsEqual(proof1, proof2 *pb.ChangeProof) bool {
	return (proof1 == nil && proof2 == nil) ||
		(proof1 != nil &&
			proof2 != nil &&
			protoKeyChangesEqual(proof1.KeyChanges, proof2.KeyChanges) &&
			protoProofNodesEqual(proof1.StartProof, proof2.StartProof) &&
			protoProofNodesEqual(proof1.EndProof, proof2.EndProof))
}

func protoKeyChangesEqual(kv1, kv2 []*pb.KeyChange) bool {
	if len(kv1) != len(kv2) {
		return false
	}
	for i, kv := range kv1 {
		if !protoKeyChangeEqual(kv, kv2[i]) {
			return false
		}
	}
	return true
}

func protoProofNodeEqual(node1, node2 *pb.ProofNode) bool {
	return (node1 == nil && node2 == nil) ||
		(node2 != nil &&
			node1 != nil &&
			protoSerializedPathEqual(node1.Key, node2.Key) &&
			protoMaybeBytesEqual(node1.ValueOrHash, node2.ValueOrHash) &&
			protoChildrenEqual(node1.Children, node2.Children))
}

func protoSerializedPathEqual(path1, path2 *pb.SerializedPath) bool {
	return (path1 == nil && path2 == nil) ||
		(path1 != nil &&
			path2 != nil &&
			bytes.Equal(path1.Value, path2.Value) &&
			path1.NibbleLength == path2.NibbleLength)
}

func protoKeyValueEqual(change1, change2 *pb.KeyValue) bool {
	return (change1 == nil && change2 == nil) ||
		(change1 != nil &&
			change2 != nil &&
			bytes.Equal(change1.Key, change2.Key) &&
			bytes.Equal(change1.Value, change2.Value))
}

func protoKeyChangeEqual(change1, change2 *pb.KeyChange) bool {
	return (change1 == nil && change2 == nil) ||
		(change1 != nil &&
			change2 != nil &&
			bytes.Equal(change1.Key, change2.Key) &&
			protoMaybeBytesEqual(change1.Value, change2.Value))
}

func protoMaybeBytesEqual(maybe1, maybe2 *pb.MaybeBytes) bool {
	return (maybe1 == nil && maybe2 == nil) ||
		(maybe1 != nil &&
			maybe2 != nil &&
			maybe1.IsNothing == maybe2.IsNothing &&
			bytes.Equal(maybe1.Value, maybe2.Value))
}

func protoChildrenEqual(children1, children2 map[uint32][]byte) bool {
	if len(children1) != len(children2) {
		return false
	}
	for key, value := range children1 {
		if otherVal, ok := children2[key]; !ok || !bytes.Equal(otherVal, value) {
			return false
		}
	}
	return true
}

func rangeProofsEqual(proof1, proof2 RangeProof) bool {
	return keyValuesEqual(proof1.KeyValues, proof2.KeyValues) &&
		proofNodesEqual(proof1.StartProof, proof2.StartProof) &&
		proofNodesEqual(proof1.EndProof, proof2.EndProof)
}

func keyValuesEqual(kv1, kv2 []KeyValue) bool {
	if len(kv1) != len(kv2) {
		return false
	}
	for i, kv := range kv1 {
		if !keyValueEqual(kv, kv2[i]) {
			return false
		}
	}
	return true
}

func proofsEqual(proof1, proof2 Proof) bool {
	if len(proof1.Path) != len(proof2.Path) {
		return false
	}

	for i := 0; i < len(proof1.Path); i++ {
		if !proofNodeEqual(proof1.Path[i], proof2.Path[i]) {
			return false
		}
	}
	return bytes.Equal(proof1.Key, proof2.Key) &&
		maybe.Equal(proof1.Value, proof2.Value, bytes.Equal)
}

func changeProofsEqual(proof1, proof2 ChangeProof) bool {
	return keyChangesEqual(proof1.KeyChanges, proof2.KeyChanges) &&
		proofNodesEqual(proof1.StartProof, proof2.StartProof) &&
		proofNodesEqual(proof1.EndProof, proof2.EndProof)
}

func keyChangesEqual(kv1, kv2 []KeyChange) bool {
	if len(kv1) != len(kv2) {
		return false
	}
	for i, kv := range kv1 {
		if !keyChangeEqual(kv, kv2[i]) {
			return false
		}
	}
	return true
}

func proofNodesEqual(proof1, proof2 []ProofNode) bool {
	if len(proof1) != len(proof2) {
		return false
	}
	for i, node := range proof1 {
		if !proofNodeEqual(node, proof2[i]) {
			return false
		}
	}
	return true
}

func proofNodeEqual(node1, node2 ProofNode) bool {
	return serializedPathEqual(node1.KeyPath, node2.KeyPath) &&
		maybe.Equal(node1.ValueOrHash, node2.ValueOrHash, bytes.Equal) &&
		childrenEqual(node1.Children, node2.Children)
}

func serializedPathEqual(path1, path2 SerializedPath) bool {
	return bytes.Equal(path1.Value, path2.Value) &&
		path1.NibbleLength == path2.NibbleLength
}

func keyValueEqual(change1, change2 KeyValue) bool {
	return bytes.Equal(change1.Key, change2.Key) &&
		bytes.Equal(change1.Value, change2.Value)
}

func keyChangeEqual(change1, change2 KeyChange) bool {
	return bytes.Equal(change1.Key, change2.Key) &&
		maybe.Equal(change1.Value, change2.Value, bytes.Equal)
}

func childrenEqual(children1, children2 map[byte]ids.ID) bool {
	if len(children1) != len(children2) {
		return false
	}
	for key, value := range children1 {
		if otherVal, ok := children2[key]; !ok || !bytes.Equal(otherVal[:], value[:]) {
			return false
		}
	}
	return true
}
