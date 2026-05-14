// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// fx exposes [secp256k1fx.Fx.VerifyTransfer] for verification of UTXO transfers
// from shared memory into the C-Chain.
var fx secp256k1fx.Fx

func init() {
	if err := fx.Initialize(fxVM{}); err != nil {
		panic(err)
	}
	// Mark the fx as bootstrapped so that it actually verifies signatures.
	if err := fx.Bootstrapped(); err != nil {
		panic(err)
	}
}

type fxVM struct {
	clock mockable.Clock
}

func (fxVM) CodecRegistry() codec.Registry { return linearcodec.NewDefault() }
func (f fxVM) Clock() *mockable.Clock      { return &f.clock }
func (fxVM) Logger() logging.Logger        { return logging.NoLog{} }

var _ secp256k1fx.UnsignedTx = (*fxTx)(nil)

type fxTx []byte

func toFxTx(u Unsigned) (fxTx, error) {
	b, err := UnsignedBytes(u)
	return fxTx(b), err
}

func (f fxTx) Bytes() []byte { return f }
