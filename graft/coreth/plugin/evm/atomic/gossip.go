// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import "github.com/ava-labs/avalanchego/network/p2p/gossip"

var _ gossip.Marshaller[*Tx] = (*TxMarshaller)(nil)

type TxMarshaller struct{}

func (*TxMarshaller) MarshalGossip(tx *Tx) ([]byte, error) {
	return tx.SignedBytes(), nil
}

func (*TxMarshaller) UnmarshalGossip(bytes []byte) (*Tx, error) {
	return ExtractAtomicTx(bytes, Codec)
}
