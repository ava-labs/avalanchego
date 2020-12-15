// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm/conflicts"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
)

var (
	_ conflicts.Tx = &tx{}
)

type tx struct {
	epoch uint32
	tx    vertex.Tx
}

func (t *tx) ID() ids.ID { return ids.Empty }

func (t *tx) Accept() error {
	return t.tx.Accept(t.epoch)
}

func (t *tx) Reject() error {
	return t.tx.Reject(t.epoch)
}

func (t *tx) Status() choices.Status {
	return choices.Unknown
}

func (t *tx) TransitionID() ids.ID { return t.tx.ID() }

func (t *tx) Epoch() uint32 { return t.epoch }

func (t *tx) Restrictions() []ids.ID { return nil }

func (t *tx) Dependencies() []ids.ID {
	return nil
}

func (t *tx) InputIDs() []ids.ID { return t.tx.InputIDs() }
