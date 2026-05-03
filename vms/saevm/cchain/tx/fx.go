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

var fx secp256k1fx.Fx

func init() {
	if err := fx.Initialize(fxVM{}); err != nil {
		panic(err)
	}
	if err := fx.Bootstrapped(); err != nil {
		panic(err)
	}
}

type fxVM struct{}

func (fxVM) CodecRegistry() codec.Registry {
	return linearcodec.NewDefault()
}

func (fxVM) Clock() *mockable.Clock {
	return &mockable.Clock{}
}

func (fxVM) Logger() logging.Logger {
	return logging.NoLog{}
}

var _ secp256k1fx.UnsignedTx = (*fxTx)(nil)

type fxTx []byte

func toFxTx(u Unsigned) (fxTx, error) {
	b, err := c.Marshal(codecVersion, &u)
	if err != nil {
		return nil, err
	}
	return fxTx(b), nil
}

func (f fxTx) Bytes() []byte { return f }
