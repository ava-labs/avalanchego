// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import "github.com/ava-labs/avalanchego/ids"

func (t *Tx) GossipID() ids.ID {
	id, err := t.ID()
	if err != nil {
		// TODO: FIXME
		panic(err)
	}
	return id
}

type Marshaller struct{}

func (Marshaller) MarshalGossip(tx *Tx) ([]byte, error)  { return tx.Bytes() }
func (Marshaller) UnmarshalGossip(b []byte) (*Tx, error) { return Parse(b) }
