// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
	"github.com/chain4travel/caminogo/snow/consensus/snowman"
	"github.com/chain4travel/caminogo/snow/engine/common/queue"
	"github.com/chain4travel/caminogo/snow/engine/snowman/block"
	"github.com/chain4travel/caminogo/utils/logging"
)

var errMissingDependenciesOnAccept = errors.New("attempting to accept a block with missing dependencies")

type parser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	vm                      block.ChainVM
}

func (p *parser) Parse(blkBytes []byte) (queue.Job, error) {
	blk, err := p.vm.ParseBlock(blkBytes)
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

func (b *blockJob) ID() ids.ID { return b.blk.ID() }
func (b *blockJob) MissingDependencies() (ids.Set, error) {
	missing := ids.Set{}
	parentID := b.blk.Parent()
	if parent, err := b.vm.GetBlock(parentID); err != nil || parent.Status() != choices.Accepted {
		missing.Add(parentID)
	}
	return missing, nil
}

func (b *blockJob) HasMissingDependencies() (bool, error) {
	parentID := b.blk.Parent()
	if parent, err := b.vm.GetBlock(parentID); err != nil || parent.Status() != choices.Accepted {
		return true, nil
	}
	return false, nil
}

func (b *blockJob) Execute() error {
	hasMissingDeps, err := b.HasMissingDependencies()
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
		if err := b.blk.Verify(); err != nil {
			b.log.Error("block %s failed verification during bootstrapping due to %s", blkID, err)
			return fmt.Errorf("failed to verify block in bootstrapping: %w", err)
		}

		b.numAccepted.Inc()
		b.log.Trace("accepting block (%s, %d) in bootstrapping", blkID, b.blk.Height())
		if err := b.blk.Accept(); err != nil {
			b.log.Debug("block %s failed to accept during bootstrapping due to %s", blkID, err)
			return fmt.Errorf("failed to accept block in bootstrapping: %w", err)
		}
	}
	return nil
}
func (b *blockJob) Bytes() []byte { return b.blk.Bytes() }
