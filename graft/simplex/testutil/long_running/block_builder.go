// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package long_running

import (
	"context"
	"sync"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
)

var _ common.BlockBuilder = (*LongRunningBlockBuilder)(nil)

type LongRunningBlockBuilder struct {
	t  *testing.T
	mu sync.Mutex

	blockPending bool
	changed      chan struct{} // signals blockPending state was changed
}

func NewNetworkBlockBuilder(t *testing.T) *LongRunningBlockBuilder {
	return &LongRunningBlockBuilder{
		t:            t,
		blockPending: true,
		changed:      make(chan struct{}, 1),
	}
}

func (b *LongRunningBlockBuilder) notifyChanged() {
	select {
	case b.changed <- struct{}{}:
	default:
	}
}

func (b *LongRunningBlockBuilder) BuildBlock(
	ctx context.Context,
	metadata common.ProtocolMetadata,
	blacklist common.Blacklist,
) (common.VerifiedBlock, bool) {
	for {
		b.mu.Lock()
		pending := b.blockPending
		b.mu.Unlock()

		if pending {
			return testutil.NewTestBlock(metadata, blacklist), true
		}

		select {
		case <-ctx.Done():
			return nil, false
		case <-b.changed:
		}
	}
}

func (b *LongRunningBlockBuilder) WaitForPendingBlock(ctx context.Context) {
	for {
		b.mu.Lock()
		pending := b.blockPending
		b.mu.Unlock()

		if pending {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-b.changed:
		}
	}
}

func (b *LongRunningBlockBuilder) TriggerBlockShouldBeBuilt() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.blockPending = true
	b.notifyChanged()
}

func (b *LongRunningBlockBuilder) TriggerBlockShouldNotBeBuilt() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.blockPending = false
	b.notifyChanged()
}
