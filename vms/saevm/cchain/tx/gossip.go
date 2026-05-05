// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import "github.com/ava-labs/avalanchego/ids"

func (t *Tx) GossipID() ids.ID {
	return t.ID()
}

type Marshaller struct{}

func (Marshaller) MarshalGossip(tx *Tx) ([]byte, error)  { return tx.Bytes() }
func (Marshaller) UnmarshalGossip(b []byte) (*Tx, error) { return Parse(b) }
