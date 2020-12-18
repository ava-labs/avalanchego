// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm/conflicts"
)

var (
	_ conflicts.Tx = &Tx{}
)

type Tx struct {
	Tr conflicts.Transition
}

func (t *Tx) ID() ids.ID { return t.Tr.ID().Prefix(0) }

func (t *Tx) Accept() error { return t.Tr.Accept(0) }

func (t *Tx) Reject() error { return t.Tr.Reject(0) }

func (t *Tx) Status() choices.Status { return t.Tr.Status() }

func (t *Tx) Transition() conflicts.Transition { return t.Tr }

func (t *Tx) Epoch() uint32 { return 0 }

func (t *Tx) Restrictions() []ids.ID { return nil }

func (t *Tx) Verify() error { return t.Tr.Verify(0) }

func (t *Tx) Bytes() []byte { return t.Tr.Bytes() }
