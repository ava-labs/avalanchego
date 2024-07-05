// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"crypto/rand"
	"encoding/binary"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	vrfOutPrefix     = "rng-derv"
	vrfRngRootPrefix = "rng-root"
)

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	blockBytes []byte,
	blockVrfSig []byte,
) (SignedBlock, error) {
	block := &statelessBlock[statelessUnsignedBlock]{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  nil,
			Block:        blockBytes,
			VRFSig:       blockVrfSig,
		},
		timestamp: timestamp,
	}
	bytes, err := marshalBlock(block)
	if err != nil {
		return nil, err
	}

	return block, block.initialize(bytes)
}

func CalculateVRFOut(vrfSig []byte) []byte {
	// build the hash of the following struct:
	// +-------------------------+----------+------------+
	// |  prefix :               | [8]byte  | "rng-derv" |
	// +-------------------------+----------+------------+
	// |  vrfSig :               | [96]byte |  96 bytes  |
	// +-------------------------+----------+------------+
	if len(vrfSig) != bls.SignatureLen {
		return nil
	}

	buffer := make([]byte, len(vrfOutPrefix)+bls.SignatureLen)
	copy(buffer, vrfOutPrefix)
	copy(buffer[len(vrfOutPrefix):], vrfSig)
	outHash := hashing.ComputeHash256Array(buffer)
	return outHash[:]
}

func initializeID(bytes []byte, signature []byte) (ids.ID, error) {
	var unsignedBytes []byte
	// The serialized form of the block is the unsignedBytes followed by the
	// signature, which is prefixed by a uint32. So, we need to strip off the
	// signature as well as it's length prefix to get the unsigned bytes.
	lenUnsignedBytes := len(bytes) - wrappers.IntLen - len(signature)

	if lenUnsignedBytes < 0 {
		return ids.Empty, errInvalidBlockEncodingLength
	}

	unsignedBytes = bytes[:lenUnsignedBytes]
	return hashing.ComputeHash256Array(unsignedBytes), nil
}

// marshalBlock marshal the given statelessBlock by using either the default statelessBlock or
// coping the exported fields into statelessBlockV0 and then marshaling it.
// this allows the marsheler to produce encoded blocks that match the old style blocks as long as
// the VRFSig feature was not enabled.
func marshalBlock(block *statelessBlock[statelessUnsignedBlock]) ([]byte, error) {
	if len(block.StatelessBlock.VRFSig) == 0 {
		// create a backward compatible block ( without VRFSig ) and use the statelessBlockV0 encoder for the encoding.
		var preBlockSigBlock SignedBlock = &statelessBlock[statelessUnsignedBlockV0]{
			StatelessBlock: statelessUnsignedBlockV0{
				ParentID:     block.StatelessBlock.ParentID,
				Timestamp:    block.StatelessBlock.Timestamp,
				PChainHeight: block.StatelessBlock.PChainHeight,
				Certificate:  block.StatelessBlock.Certificate,
				Block:        block.StatelessBlock.Block,
			},
			Signature: block.Signature,
		}
		return Codec.Marshal(CodecVersion, &preBlockSigBlock)
	}
	var blockIntf SignedBlock = block
	return Codec.Marshal(CodecVersion, &blockIntf)
}

func calculateBootstrappingBlockSig(chainID ids.ID, networkID uint32) [hashing.HashLen]byte {
	// build the hash of the following struct:
	// +-----------------------+----------+------------+
	// |  prefix :             | [8]byte  | "rng-root" |
	// +-----------------------+----------+------------+
	// |  chainID :            | [32]byte |  32 bytes  |
	// +-----------------------+----------+------------+
	// |  networkID:           | uint32   |  4 bytes   |
	// +-----------------------+----------+------------+

	buffer := make([]byte, len(vrfRngRootPrefix)+ids.IDLen+4)
	copy(buffer, vrfRngRootPrefix)
	copy(buffer[len(vrfRngRootPrefix):], chainID[:])
	binary.LittleEndian.PutUint32(buffer[len(vrfRngRootPrefix)+ids.IDLen:], networkID)
	return hashing.ComputeHash256Array(buffer)
}

func NextHashBlockSignature(parentBlockSig []byte) []byte {
	if len(parentBlockSig) == 0 {
		return nil
	}
	// previous block had a valid signature, hash that signature.
	sigParentBlockSig := hashing.ComputeHash256(parentBlockSig)

	// as long as the signature length is too short, generate additional hashes.
	for len(sigParentBlockSig) < len(parentBlockSig) {
		sigParentBlockSig = append(sigParentBlockSig, hashing.ComputeHash256(sigParentBlockSig)...)
	}

	// adjust the size of the hash to be as long as the parent signature ( which is a BLS signature length )
	sigParentBlockSig = sigParentBlockSig[:len(parentBlockSig)]

	return sigParentBlockSig
}

func NextBlockVRFSig(parentBlockVRFSig []byte, blsSignKey *bls.SecretKey, chainID ids.ID, networkID uint32) []byte {
	if blsSignKey == nil {
		// if we need to build a block without having a BLS key, we'll be hashing the previous
		// signature only if it presents. Otherwise, we'll keep it empty.
		if len(parentBlockVRFSig) == 0 {
			// no parent block signature.
			return []byte{}
		}

		return NextHashBlockSignature(parentBlockVRFSig)
	}

	// we have bls key
	var signMsg []byte
	if len(parentBlockVRFSig) == 0 {
		msgHash := calculateBootstrappingBlockSig(chainID, networkID)
		signMsg = msgHash[:]
	} else {
		signMsg = parentBlockVRFSig
	}

	return bls.SignatureToBytes(bls.Sign(blsSignKey, signMsg))
}

func Build(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	cert *staking.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	key crypto.Signer,
	blockVrfSig []byte,
) (SignedBlock, error) {
	block := &statelessBlock[statelessUnsignedBlock]{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  cert.Raw,
			Block:        blockBytes,
			VRFSig:       blockVrfSig,
		},
		timestamp: timestamp,
		cert:      cert,
		proposer:  ids.NodeIDFromCert(cert),
	}

	var err error

	var blkBytes []byte
	// temporary, set the bytes to the marshaled content of the block.
	// this doesn't include the signature ( yet )
	if blkBytes, err = marshalBlock(block); err != nil {
		return nil, err
	}

	// calculate the block ID.
	if block.id, err = initializeID(blkBytes, nil); err != nil {
		return nil, err
	}

	// use the block ID in order to build the header.
	header, err := BuildHeader(chainID, parentID, block.id)
	if err != nil {
		return nil, err
	}

	headerHash := hashing.ComputeHash256(header.Bytes())
	block.Signature, err = key.Sign(rand.Reader, headerHash, crypto.SHA256)
	if err != nil {
		return nil, err
	}

	block.bytes, err = marshalBlock(block)
	return block, err
}

func BuildHeader(
	chainID ids.ID,
	parentID ids.ID,
	bodyID ids.ID,
) (Header, error) {
	header := statelessHeader{
		Chain:  chainID,
		Parent: parentID,
		Body:   bodyID,
	}

	bytes, err := Codec.Marshal(CodecVersion, &header)
	header.bytes = bytes
	return &header, err
}

// BuildOption the option block
// [parentID] is the ID of this option's wrapper parent block
// [innerBytes] is the byte representation of a child option block
func BuildOption(
	parentID ids.ID,
	innerBytes []byte,
) (Block, error) {
	var block Block = &option{
		PrntID:     parentID,
		InnerBytes: innerBytes,
	}

	bytes, err := Codec.Marshal(CodecVersion, &block)
	if err != nil {
		return nil, err
	}

	return block, block.initialize(bytes)
}
