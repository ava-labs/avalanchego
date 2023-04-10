// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errMissingDependenciesOnAccept = errors.New("attempting to accept a block with missing dependencies")

type parser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	vm                      block.ChainVM
}

func (p *parser) Parse(ctx context.Context, blkBytes []byte) (queue.Job, error) {
	blk, err := p.vm.ParseBlock(ctx, blkBytes)
	if err != nil {
		return nil, err
	}
	return &blockJob{
		parser:      p,
		log:         p.log,
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		blk:         blk,
		vm:          p.vm,
	}, nil
}

type blockJob struct {
	parser                  *parser
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	blk                     snowman.Block
	vm                      block.Getter
}

func (b *blockJob) ID() ids.ID {
	return b.blk.ID()
}

func (b *blockJob) MissingDependencies(ctx context.Context) (set.Set[ids.ID], error) {
	missing := set.Set[ids.ID]{}
	parentID := b.blk.Parent()
	if parent, err := b.vm.GetBlock(ctx, parentID); err != nil || parent.Status() != choices.Accepted {
		missing.Add(parentID)
	}
	return missing, nil
}

func (b *blockJob) HasMissingDependencies(ctx context.Context) (bool, error) {
	parentID := b.blk.Parent()
	if parent, err := b.vm.GetBlock(ctx, parentID); err != nil || parent.Status() != choices.Accepted {
		return true, nil
	}
	return false, nil
}

func (b *blockJob) Execute(ctx context.Context) error {
	hasMissingDeps, err := b.HasMissingDependencies(ctx)
	if err != nil {
		return err
	}
	if hasMissingDeps {
		b.numDropped.Inc()
		return errMissingDependenciesOnAccept
	}
	status := b.blk.Status()
	switch status {
	case choices.Unknown, choices.Rejected:
		b.numDropped.Inc()
		return fmt.Errorf("attempting to execute block with status %s", status)
	case choices.Processing:
		blkID := b.blk.ID()
		if err := b.blk.Verify(ctx); err != nil {
			b.log.Error("block failed verification during bootstrapping",
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
			return fmt.Errorf("failed to verify block in bootstrapping: %w", err)
		}

		b.numAccepted.Inc()
		b.log.Trace("accepting block in bootstrapping",
			zap.Stringer("blkID", blkID),
			zap.Uint64("blkHeight", b.blk.Height()),
		)
		if err := b.blk.Accept(ctx); err != nil {
			b.log.Debug("failed to accept block during bootstrapping",
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
			return fmt.Errorf("failed to accept block in bootstrapping: %w", err)
		}
	}
	return nil
}

func (b *blockJob) Bytes() []byte {
	return b.blk.Bytes()
}
