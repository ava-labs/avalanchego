// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"sync"

	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
)

type ParsedBlock struct {
	metadata.StateMachineBlock
	msm *metadata.StateMachine

	legacyBlock bool // true if this is not a simplex block, but a block pre-dating simplex.

	// lock guards size, so Size() can be invoked concurrently
	lock sync.Mutex
	// size caches the length of the Bytes encoding, computed on first use
	size int
}

func (p *ParsedBlock) Bytes() []byte {
	var innerBlockBytes []byte
	if p.InnerBlock != nil {
		rawInnerBlock := p.InnerBlock.Bytes()
		innerBlockBytes = rawInnerBlock
	}

	if p.legacyBlock {
		return innerBlockBytes
	}

	rawBlock := &metadata.RawBlock{
		Metadata:        p.Metadata.Clone(),
		InnerBlockBytes: innerBlockBytes,
	}
	return rawBlock.MarshalCanoto()
}

func (p *ParsedBlock) BlockHeader() common.BlockHeader {
	md := p.Metadata.SimplexProtocolMetadata.Clone()
	digest := p.Digest()

	if p.legacyBlock {
		digest = p.InnerBlock.Digest()
	}

	return common.BlockHeader{
		ProtocolMetadata: md,
		Digest:           digest,
	}
}

func (p *ParsedBlock) Blacklist() common.Blacklist {
	return p.Metadata.SimplexBlacklist.Clone()
}

func (p *ParsedBlock) Verify(ctx context.Context, verifyOpts ...common.VerifyOptions) (common.VerifiedBlock, error) {
	var verifyOptions common.VerifyConfig
	for _, opt := range verifyOpts {
		opt(&verifyOptions)
	}

	// When replicating a block that carries a notarization or a finalization, the quorum certificate
	// already attests to the state machine transition, so we only verify the inner block.
	if verifyOptions.OnlyVM {
		if p.InnerBlock == nil {
			return p, nil
		}
		return p, p.InnerBlock.Verify(ctx, p.Metadata.ICMEpochInfo.PChainEpochHeight)
	}

	if err := p.msm.VerifyBlock(ctx, &p.StateMachineBlock); err != nil {
		return nil, err
	}
	return p, nil
}
func (p *ParsedBlock) Size() int {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.size == 0 {
		bytes := p.Bytes()
		p.size = len(bytes)
	}
	return p.size
}
