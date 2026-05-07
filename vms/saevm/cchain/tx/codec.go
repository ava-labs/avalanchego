// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const codecVersion uint16 = 0

var c codec.Manager

func init() {
	c = codec.NewDefaultManager()

	// Registration order impacts the typeID included in the canonical format.
	// We skip registrations in specific locations so that UTXOs in shared
	// memory share the same serialized format as on the P-Chain and X-Chain.
	var (
		lc   = linearcodec.NewDefault()
		errs = wrappers.Errs{}
	)
	errs.Add(
		lc.RegisterType(&Import{}),
		lc.RegisterType(&Export{}),
	)
	lc.SkipRegistrations(3)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferInput{}),
	)
	lc.SkipRegistrations(1)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferOutput{}),
	)
	lc.SkipRegistrations(1)
	errs.Add(
		lc.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// MarshalSlice returns the canonical binary format of a slice of transactions.
func MarshalSlice(txs []*Tx) ([]byte, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	return c.Marshal(codecVersion, txs)
}

var errInefficientSlicePacking = errors.New("inefficient slice packing: empty slices should be packed as nil")

// ParseSlice deserializes a slice of [Tx] from its canonical binary format.
func ParseSlice(b []byte) ([]*Tx, error) {
	if len(b) == 0 {
		return nil, nil
	}

	var txs []*Tx
	if _, err := c.Unmarshal(b, &txs); err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, errInefficientSlicePacking
	}
	return txs, nil
}
