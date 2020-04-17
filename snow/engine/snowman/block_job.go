// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
)

type parser struct {
	numAccepted, numDropped prometheus.Counter
	vm                      ChainVM
}

func (p *parser) Parse(blkBytes []byte) (queue.Job, error) {
	blk, err := p.vm.ParseBlock(blkBytes)
	if err != nil {
		return nil, err
	}
	return &blockJob{
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		blk:         blk,
	}, nil
}

type blockJob struct {
	numAccepted, numDropped prometheus.Counter
	blk                     snowman.Block
}

func (b *blockJob) ID() ids.ID { return b.blk.ID() }
func (b *blockJob) MissingDependencies() ids.Set {
	missing := ids.Set{}
	if parent := b.blk.Parent(); parent.Status() != choices.Accepted {
		missing.Add(parent.ID())
	}
	return missing
}
func (b *blockJob) Execute() {
	if b.MissingDependencies().Len() != 0 {
		b.numDropped.Inc()
		return
	}
	switch b.blk.Status() {
	case choices.Unknown, choices.Rejected:
		b.numDropped.Inc()
	case choices.Processing:
		b.blk.Verify()
		b.blk.Accept()
		b.numAccepted.Inc()
	}
}
func (b *blockJob) Bytes() []byte { return b.blk.Bytes() }
