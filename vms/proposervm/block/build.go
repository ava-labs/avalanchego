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
)

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	blockBytes []byte,
	chainID ids.ID,
	networkID uint32,
	parentBlockSig []byte,
	blsSignKey *bls.SecretKey,
) (SignedBlock, error) {
	sigParentBlockSig := nextBlockSignature(parentBlockSig, blsSignKey, chainID, networkID)

	block := &statelessBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  nil,
			Block:        blockBytes,
			VRFSig:       sigParentBlockSig,
		},
		timestamp: timestamp,
	}
	bytes, err := marshalBlock(block)
	if err != nil {
		return nil, err
	}

	return block, block.initialize(bytes)
}

// marshalBlock marshal the given statelessBlock by using either the default statelessBlock or
// coping the exported fields into statelessBlockV0 and then marshaling it.
// this allows the marsheler to produce encoded blocks that match the old style blocks as long as
// the VRGSig feature was not enabled.
func marshalBlock(block *statelessBlock) ([]byte, error) {
	if len(block.StatelessBlock.VRFSig) == 0 {
		// create a backward compatible block ( without SignedParentBlockSig ) and use the PreBlockSigCodecVersion encoder for the encoding.
		var preBlockSigBlock SignedBlock = &statelessBlockV0{
			StatelessBlock: statelessUnsignedBlockV0{
				ParentID:     block.StatelessBlock.ParentID,
				Timestamp:    block.StatelessBlock.Timestamp,
				PChainHeight: block.StatelessBlock.PChainHeight,
				Certificate:  nil,
				Block:        block.StatelessBlock.Block,
			},
		}
		return Codec.Marshal(CodecVersion, &preBlockSigBlock)
	}
	var blockIntf SignedBlock = block
	return Codec.Marshal(CodecVersion, &blockIntf)
}

func CalculateBootstrappingBlockSig(chainID ids.ID, networkID uint32) [hashing.HashLen]byte {
	// build the hash of the following struct:
	// +-----------------------+----------+------------+
	// |  prefix :             | [8]byte  | "rng-root" |
	// +-----------------------+----------+------------+
	// |  chainID :            | [32]byte |  32 bytes  |
	// +-----------------------+----------+------------+
	// |  networkID:           | uint32   |  4 bytes   |
	// +-----------------------+----------+------------+

	buffer := make([]byte, 44)
	copy(buffer, "rng-root")
	copy(buffer[8:], chainID[:])
	binary.LittleEndian.PutUint32(buffer[40:], networkID)
	return hashing.Hash256(buffer)
}

func nextHashBlockSignature(parentBlockSig []byte) []byte {
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

func nextBlockSignature(parentBlockSig []byte, blsSignKey *bls.SecretKey, chainID ids.ID, networkID uint32) []byte {
	if blsSignKey == nil {
		// if we need to build a block without having a BLS key, we'll be hashing the previous
		// signature only if it presents. Otherwise, we'll keep it empty.
		if len(parentBlockSig) == 0 {
			// no parent block signature.
			return []byte{}
		}

		return nextHashBlockSignature(parentBlockSig)
	}

	// we have bls key
	var signMsg []byte
	if parentBlockSig == nil {
		msgHash := CalculateBootstrappingBlockSig(chainID, networkID)
		signMsg = msgHash[:]
	} else {
		signMsg = parentBlockSig
	}

	return bls.Sign(blsSignKey, signMsg).Serialize()
}

func Build(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	cert *staking.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	networkID uint32,
	key crypto.Signer,
	parentBlockSig []byte,
	blsSignKey *bls.SecretKey,
) (SignedBlock, error) {
	sigParentBlockSig := nextBlockSignature(parentBlockSig, blsSignKey, chainID, networkID)

	block := &statelessBlock{
		StatelessBlock: statelessUnsignedBlock{
			ParentID:     parentID,
			Timestamp:    timestamp.Unix(),
			PChainHeight: pChainHeight,
			Certificate:  cert.Raw,
			Block:        blockBytes,
			VRFSig:       sigParentBlockSig,
		},
		timestamp: timestamp,
		cert:      cert,
		proposer:  ids.NodeIDFromCert(cert),
	}

	var err error

	// temporary, set the bytes to the marshaled content of the block.
	// this doesn't include the signature ( yet )
	if block.bytes, err = marshalBlock(block); err != nil {
		return nil, err
	}

	// calculate the block ID.
	if err = block.initializeID(); err != nil {
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
