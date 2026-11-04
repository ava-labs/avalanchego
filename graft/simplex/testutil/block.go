// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"time"

	"github.com/ava-labs/simplex/common"
)

type TestBlock struct {
	Data              []byte
	Metadata          common.ProtocolMetadata
	blacklist         common.Blacklist
	Digest            common.Digest
	OnVerify          func()
	VerificationDelay chan struct{}
	VerificationError error
	// SealingInfo, when non-nil, is returned by SealingBlockInfo() to mark this block as a sealing block.
	SealingInfo *common.SealingBlockInfo
}

func NewTestBlock(metadata common.ProtocolMetadata, blacklist common.Blacklist) *TestBlock {
	tb := TestBlock{
		blacklist: blacklist,
		Metadata:  metadata,
	}

	tb.ComputeDigest()

	return &tb
}

func (tb *TestBlock) Blacklist() common.Blacklist {
	return tb.blacklist
}

func (tb *TestBlock) Verify(context.Context, ...common.VerifyOptions) (common.VerifiedBlock, error) {
	defer func() {
		if tb.OnVerify != nil {
			tb.OnVerify()
		}
	}()

	if tb.VerificationError != nil {
		return nil, tb.VerificationError
	}

	if tb.VerificationDelay == nil {
		return tb, nil
	}

	<-tb.VerificationDelay

	return tb, nil
}

func (tb *TestBlock) SealingBlockInfo() *common.SealingBlockInfo {
	return tb.SealingInfo
}

func (tb *TestBlock) ComputeDigest() {
	var bb bytes.Buffer
	tbBytes := tb.Bytes()
	bb.Write(tbBytes)
	tb.Digest = sha256.Sum256(bb.Bytes())
}

func (t *TestBlock) BlockHeader() common.BlockHeader {
	return common.BlockHeader{
		ProtocolMetadata: t.Metadata,
		Digest:           t.Digest,
	}
}

func (t *TestBlock) Bytes() []byte {
	bh := common.BlockHeader{
		ProtocolMetadata: t.Metadata,
	}

	mdBuff := bh.ProtocolMetadata.Bytes()

	blBytes := t.blacklist.Bytes()

	if bytes.Equal(blBytes, t.Data) {
		t.Data = nil
	}

	rawBytes, err := asn1.Marshal(EncodedTestBlock{
		Data:      t.Data,
		Metadata:  mdBuff,
		Blacklist: blBytes,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to serialize test block: %v", err))
	}

	return rawBytes
}

func (t *TestBlock) Size() int {
	return len(t.Bytes())
}

type EncodedTestBlock struct {
	Data      []byte
	Metadata  []byte
	Blacklist []byte
}

type BlockDeserializer struct {
	// DelayedVerification will block verifying any deserialized blocks until we send to the channel
	DelayedVerification chan struct{}
}

func (b *BlockDeserializer) DeserializeBlock(ctx context.Context, buff []byte) (common.Block, error) {
	var encodedBlock EncodedTestBlock
	_, err := asn1.Unmarshal(buff, &encodedBlock)
	if err != nil {
		return nil, err
	}

	md, err := common.ProtocolMetadataFromBytes(encodedBlock.Metadata)
	if err != nil {
		return nil, err
	}

	var blacklist common.Blacklist
	if err := blacklist.FromBytes(encodedBlock.Blacklist); err != nil {
		return nil, err
	}

	tb := TestBlock{
		blacklist:         blacklist,
		Metadata:          *md,
		VerificationDelay: b.DelayedVerification,
	}

	if len(encodedBlock.Data) > 0 {
		tb.Data = encodedBlock.Data
	}

	tb.ComputeDigest()

	return &tb, nil
}

type InnerBlock struct {
	TS          time.Time
	BlockHeight uint64
	Content     []byte
}

func (i *InnerBlock) Bytes() []byte {
	return i.Content
}

func (i *InnerBlock) Digest() [32]byte {
	return sha256.Sum256(i.Content)
}

func (i *InnerBlock) Height() uint64 {
	return i.BlockHeight
}

func (i *InnerBlock) Timestamp() time.Time {
	return i.TS
}

func (i *InnerBlock) Verify(_ context.Context, _ uint64) error {
	return nil
}
