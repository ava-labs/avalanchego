// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// commonBlock contains fields and methods common to all full blocks in this VM.
type commonBlock struct {
	timestampGetter
	state.LastAccepteder
	baseBlk   *stateless.CommonBlock
	timestamp time.Time // Time this block was proposed at
	status    choices.Status
	// TODO remove
	// children  []Block
}

/* TODO remove
func (c *commonBlock) addChild(child Block) {
	c.children = append(c.children, child)
}
*/

// Parent returns this block's parent's ID
func (c *commonBlock) Status() choices.Status { return c.status }

func (c *commonBlock) Timestamp() time.Time {
	// If this is the last accepted block and the block was loaded from disk
	// since it was accepted, then the timestamp wouldn't be set correctly. So,
	// we explicitly return the chain time.
	if c.baseBlk.ID() == c.GetLastAccepted() {
		return c.GetTimestamp()
	}
	return c.timestamp
}
