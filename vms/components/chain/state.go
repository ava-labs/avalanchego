// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/prometheus/client_golang/prometheus"
)

// Block is an interface wrapping the normal snowman.Block interface to be used in
// association with passing in a non-nil function to GetBlockIDAtHeight
type Block interface {
	snowman.Block

	SetStatus(choices.Status)
}

type StateConfig struct {
	Config
	GetBlockIDAtHeight func(uint64) (ids.ID, error)
}

func produceGetStatus(c *Cache, getBlockIDAtHeight func(uint64) (ids.ID, error)) func(snowman.Block) (choices.Status, error) {
	return func(blk snowman.Block) (choices.Status, error) {
		internalBlk, ok := blk.(Block)
		if !ok {
			return choices.Unknown, fmt.Errorf("expected block to match chain Block interface but found block of type %T", blk)
		}
		lastAcceptedHeight := c.lastAcceptedBlock.Height()
		blkHeight := internalBlk.Height()
		if blkHeight > lastAcceptedHeight {
			internalBlk.SetStatus(choices.Processing)
			return choices.Processing, nil
		}

		acceptedID, err := getBlockIDAtHeight(blkHeight)
		if err != nil {
			return choices.Unknown, fmt.Errorf("failed to get accepted blkID at height %d", blkHeight)
		}
		if acceptedID == blk.ID() {
			internalBlk.SetStatus(choices.Accepted)
			return choices.Accepted, nil
		}

		internalBlk.SetStatus(choices.Rejected)
		return choices.Rejected, nil
	}
}

func NewState(config *StateConfig) *Cache {
	c := NewCache(&config.Config)
	c.getStatus = produceGetStatus(c, config.GetBlockIDAtHeight)
	return c
}

func NewMeteredState(
	registerer prometheus.Registerer,
	namespace string,
	config *StateConfig,
) (*Cache, error) {
	c, err := NewMeteredCache(registerer, namespace, &config.Config)
	if err != nil {
		return nil, err
	}
	c.getStatus = produceGetStatus(c, config.GetBlockIDAtHeight)
	return c, nil
}
