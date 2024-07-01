// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

func NewWalletFactory(
	ctx *snow.Context,
	cfg *config.Config,
	clk *mockable.Clock,
	state state.State,
) *WalletFactory {
	return &WalletFactory{
		ctx:   ctx,
		cfg:   cfg,
		clk:   clk,
		state: state,
	}
}

type WalletFactory struct {
	ctx   *snow.Context
	cfg   *config.Config
	clk   *mockable.Clock
	state state.State
}

func (w *WalletFactory) NewWallet(keys ...*secp256k1.PrivateKey) (builder.Builder, signer.Signer, *fee.Calculator, error) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrs   = kc.Addresses()
		backend = newBackend(addrs, w.state, w.ctx.SharedMemory)
		context = newContext(w.ctx, w.cfg, w.state.GetTimestamp())

		builder      = builder.New(addrs, context, backend)
		signer       = signer.New(kc, backend)
		feeCalc, err = w.FeeCalculator()
	)

	return builder, signer, feeCalc, err
}

func (w *WalletFactory) FeeCalculator() (*fee.Calculator, error) {
	parentBlkTime := w.state.GetTimestamp()
	nextBlkTime, _, err := state.NextBlockTime(w.state, w.clk)
	if err != nil {
		return nil, fmt.Errorf("failed calculating next block time: %w", err)
	}

	diff, err := state.NewDiffOn(w.state)
	if err != nil {
		return nil, fmt.Errorf("failed building diff: %w", err)
	}
	diff.SetTimestamp(nextBlkTime)

	return state.PickFeeCalculator(w.cfg, diff, parentBlkTime)
}
