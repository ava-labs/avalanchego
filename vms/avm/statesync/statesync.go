package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/x/sync"
)

type State interface {
	Accept(StateSummary) error
}

type StateSummary struct {
	MerkleRoot  ids.ID
	BlockHeight uint64
	Syncer      sync.Manager

	initialized bool
	bytes       []byte
	id          ids.ID
}

func (s StateSummary) tryInit() {
	if s.initialized {
		return
	}

	//TODO init bytes
	//TODO init id
}

func (s StateSummary) ID() ids.ID {
	s.tryInit()

	return s.id
}

func (s StateSummary) Height() uint64 {
	return s.BlockHeight
}

func (s StateSummary) Bytes() []byte {
	s.tryInit()
	return s.bytes
}

func (s StateSummary) Accept(ctx context.Context) (block.StateSyncMode, error) {
	//TODO implement me
	panic("implement me")
}
