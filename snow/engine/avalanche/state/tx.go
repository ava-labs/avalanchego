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
	tx vertex.StatelessTx
	tr conflicts.Transition
}

func (t *tx) ID() ids.ID { return t.tx.ID() }

func (t *tx) Accept() error { return t.tr.Accept(t.tx.Epoch()) }

func (t *tx) Reject() error { return t.tr.Reject(t.tx.Epoch()) }

// TODO: This status implementation might not make sense
func (t *tx) Status() choices.Status { return t.tr.Status() }

func (t *tx) Transition() conflicts.Transition { return t.tr }

func (t *tx) Epoch() uint32 { return t.tr.Epoch() }

func (t *tx) Restrictions() []ids.ID { return t.tx.Restrictions() }

func (t *tx) Verify() error { return t.tr.Verify(t.tx.Epoch()) }

func (t *tx) Bytes() []byte { return t.tx.Bytes() }
