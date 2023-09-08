package merkledb

import (
	"bytes"

	"github.com/ava-labs/avalanchego/ids"
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

func protoProofsEqual(proof1, proof2 *pb.Proof) bool {
	if (proof1 == nil && proof2 != nil) || (proof2 == nil && proof1 != nil) {
		return false
	}
	if proof1 == nil && proof2 == nil {
		return true
	}
	if len(proof1.Proof) != len(proof2.Proof) {
		return false
	}

	for i := 0; i < len(proof1.Proof); i++ {
		if !protoProofNodeEqual(proof1.Proof[i], proof2.Proof[i]) {
			return false
		}
	}
	return bytes.Equal(proof1.Key, proof2.Key) && protoMaybeBytesEqual(proof1.Value, proof2.Value)
}

func protoRangeProofsEqual(proof1, proof2 *pb.RangeProof) bool {
	if (proof1 == nil && proof2 != nil) || (proof2 == nil && proof1 != nil) {
		return false
	}
	if proof1 == nil && proof2 == nil {
		return true
	}
	if len(proof1.Start) != len(proof2.Start) {
		return false
	}
	if len(proof1.End) != len(proof2.End) {
		return false
	}
	if len(proof1.KeyValues) != len(proof2.KeyValues) {
		return false
	}

	for i := 0; i < len(proof1.Start); i++ {
		if !protoProofNodeEqual(proof1.Start[i], proof2.Start[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.End); i++ {
		if !protoProofNodeEqual(proof1.End[i], proof2.End[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.KeyValues); i++ {
		if !protoKeyValueEqual(proof1.KeyValues[i], proof2.KeyValues[i]) {
			return false
		}
	}
	return true
}

func protoChangeProofsEqual(proof1, proof2 *pb.ChangeProof) bool {
	if (proof1 == nil && proof2 != nil) || (proof2 == nil && proof1 != nil) {
		return false
	}
	if proof1 == nil && proof2 == nil {
		return true
	}
	if len(proof1.StartProof) != len(proof2.StartProof) {
		return false
	}
	if len(proof1.EndProof) != len(proof2.EndProof) {
		return false
	}
	if len(proof1.KeyChanges) != len(proof2.KeyChanges) {
		return false
	}

	for i := 0; i < len(proof1.StartProof); i++ {
		if !protoProofNodeEqual(proof1.StartProof[i], proof2.StartProof[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.EndProof); i++ {
		if !protoProofNodeEqual(proof1.EndProof[i], proof2.EndProof[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.KeyChanges); i++ {
		if !protoKeyChangeEqual(proof1.KeyChanges[i], proof2.KeyChanges[i]) {
			return false
		}
	}
	return true
}

func protoProofNodeEqual(node1, node2 *pb.ProofNode) bool {
	if (node1 == nil && node2 != nil) || (node2 == nil && node1 != nil) {
		return false
	}
	if node1 == nil && node2 == nil {
		return true
	}

	return protoSerializedPathEqual(node1.Key, node2.Key) &&
		protoMaybeBytesEqual(node1.ValueOrHash, node2.ValueOrHash) &&
		protoChildrenEqual(node1.Children, node2.Children)
}

func protoSerializedPathEqual(path1, path2 *pb.SerializedPath) bool {
	return (path1 == nil && path2 == nil) || (path1 != nil && path2 != nil && bytes.Equal(path1.Value, path2.Value) && path1.NibbleLength == path2.NibbleLength)
}

func protoKeyValueEqual(change1, change2 *pb.KeyValue) bool {
	return (change1 == nil && change2 == nil) || (change1 != nil && change2 != nil && bytes.Equal(change1.Key, change2.Key) && bytes.Equal(change1.Value, change2.Value))
}

func protoKeyChangeEqual(change1, change2 *pb.KeyChange) bool {
	return (change1 == nil && change2 == nil) || (change1 != nil && change2 != nil && bytes.Equal(change1.Key, change2.Key) && protoMaybeBytesEqual(change1.Value, change2.Value))
}

func protoMaybeBytesEqual(maybe1, maybe2 *pb.MaybeBytes) bool {
	if (maybe1 == nil && maybe2 != nil) || (maybe2 == nil && maybe1 != nil) {
		return false
	}
	if maybe1 == nil && maybe2 == nil {
		return true
	}

	return maybe1.IsNothing == maybe2.IsNothing && bytes.Equal(maybe1.Value, maybe2.Value)
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
	if len(proof1.StartProof) != len(proof2.StartProof) {
		return false
	}
	if len(proof1.EndProof) != len(proof2.EndProof) {
		return false
	}
	if len(proof1.KeyValues) != len(proof2.KeyValues) {
		return false
	}

	for i := 0; i < len(proof1.StartProof); i++ {
		if !proofNodeEqual(proof1.StartProof[i], proof2.StartProof[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.EndProof); i++ {
		if !proofNodeEqual(proof1.EndProof[i], proof2.EndProof[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.KeyValues); i++ {
		if !keyValueEqual(proof1.KeyValues[i], proof2.KeyValues[i]) {
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
	return bytes.Equal(proof1.Key, proof2.Key) && maybe.Equal(proof1.Value, proof2.Value, bytes.Equal)
}

func changeProofsEqual(proof1, proof2 ChangeProof) bool {
	if len(proof1.StartProof) != len(proof2.StartProof) {
		return false
	}
	if len(proof1.EndProof) != len(proof2.EndProof) {
		return false
	}
	if len(proof1.KeyChanges) != len(proof2.KeyChanges) {
		return false
	}

	for i := 0; i < len(proof1.StartProof); i++ {
		if !proofNodeEqual(proof1.StartProof[i], proof2.StartProof[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.EndProof); i++ {
		if !proofNodeEqual(proof1.EndProof[i], proof2.EndProof[i]) {
			return false
		}
	}
	for i := 0; i < len(proof1.KeyChanges); i++ {
		if !keyChangeEqual(proof1.KeyChanges[i], proof2.KeyChanges[i]) {
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
	return bytes.Equal(path1.Value, path2.Value) && path1.NibbleLength == path2.NibbleLength
}

func keyValueEqual(change1, change2 KeyValue) bool {
	return bytes.Equal(change1.Key, change2.Key) && bytes.Equal(change1.Value, change2.Value)
}

func keyChangeEqual(change1, change2 KeyChange) bool {
	return bytes.Equal(change1.Key, change2.Key) && maybe.Equal(change1.Value, change2.Value, bytes.Equal)
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
