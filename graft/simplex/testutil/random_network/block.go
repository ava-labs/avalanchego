// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"context"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"

	"github.com/ava-labs/simplex/common"
)

var _ common.Block = (*Block)(nil)

type Block struct {
	blacklist common.Blacklist

	// contents
	txs []*TX

	// protocol metadata
	metadata common.ProtocolMetadata
	digest   common.Digest

	// mempool access
	mempool *Mempool
}

func NewBlock(metadata common.ProtocolMetadata, blacklist common.Blacklist, mempool *Mempool, txs []*TX) *Block {
	b := &Block{
		mempool:   mempool,
		txs:       txs,
		metadata:  metadata,
		blacklist: blacklist,
	}

	b.ComputeAndSetDigest()
	return b
}

func (b *Block) Verify(ctx context.Context, _ ...common.VerifyOptions) (common.VerifiedBlock, error) {
	return b, b.mempool.VerifyBlock(ctx, b)
}

func (b *Block) Blacklist() common.Blacklist {
	return b.blacklist
}

func (b *Block) BlockHeader() common.BlockHeader {
	return common.BlockHeader{
		ProtocolMetadata: b.metadata,
		Digest:           b.digest,
	}
}

func (b *Block) SealingBlockInfo() *common.SealingBlockInfo {
	return nil
}

type encodedBlock struct {
	ProtocolMetadata []byte
	TXs              []asn1TX
	Blacklist        []byte
}

func (b *Block) Bytes() []byte {
	mdBytes := b.metadata.Bytes()

	var asn1TXs []asn1TX
	for _, tx := range b.txs {
		asn1TXs = append(asn1TXs, asn1TX{ID: tx.ID[:], ShouldFailVerification: tx.shouldFailVerification})
	}

	blacklistBytes := b.blacklist.Bytes()

	encodedB := encodedBlock{
		ProtocolMetadata: mdBytes,
		TXs:              asn1TXs,
		Blacklist:        blacklistBytes,
	}
	raw, err := asn1.Marshal(encodedB)
	if err != nil {
		panic(fmt.Sprintf("failed to serialize block: %v", err))
	}
	return raw
}

func (b *Block) Size() int {
	return len(b.Bytes())
}

func (b *Block) containsTX(txID txID) bool {
	for _, tx := range b.txs {
		if tx.ID == txID {
			return true
		}
	}
	return false
}

func (b *Block) ComputeAndSetDigest() {
	b.digest = sha256.Sum256(b.Bytes())
}

type BlockDeserializer struct {
	mempool *Mempool
}

var _ common.BlockDeserializer = (*BlockDeserializer)(nil)

func (bd *BlockDeserializer) DeserializeBlock(ctx context.Context, buff []byte) (common.Block, error) {
	var encodedBlock encodedBlock
	_, err := asn1.Unmarshal(buff, &encodedBlock)
	if err != nil {
		return nil, err
	}

	md, err := common.ProtocolMetadataFromBytes(encodedBlock.ProtocolMetadata)
	if err != nil {
		return nil, err
	}

	var blacklist common.Blacklist
	if err := blacklist.FromBytes(encodedBlock.Blacklist); err != nil {
		return nil, err
	}

	txs := make([]*TX, len(encodedBlock.TXs))
	for i, asn1Tx := range encodedBlock.TXs {
		tx := asn1Tx.toTX()
		txs[i] = tx
	}

	b := &Block{
		metadata:  *md,
		txs:       txs,
		blacklist: blacklist,
		mempool:   bd.mempool,
	}

	b.ComputeAndSetDigest()

	return b, nil
}
