// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ TxBuilder = &builder{}

type TxBuilder interface {
	AtomicTxBuilder
	DecisionsTxBuilder
	ProposalsTxBuilder

	ResetAtomicUTXOManager(aum avax.AtomicUTXOManager) // useful for UTs. TODO ABENEGIA: consider find a way to drop this
}

func NewTxBuilder(
	ctx *snow.Context,
	cfg config.Config,
	clk mockable.Clock,
	fx fx.Fx,
	state state.Mutable,
	atoUtxosMan avax.AtomicUTXOManager,
	spendingOps utxos.SpendingOps,
	rewards reward.Calculator,
) TxBuilder {
	return &builder{
		AtomicUTXOManager: atoUtxosMan,
		SpendingOps:       spendingOps,
		state:             state,
		cfg:               cfg,
		ctx:               ctx,
		clk:               clk,
		fx:                fx,
		rewards:           rewards,
	}
}

type builder struct {
	avax.AtomicUTXOManager
	utxos.SpendingOps
	state state.Mutable

	cfg     config.Config
	ctx     *snow.Context
	clk     mockable.Clock
	fx      fx.Fx
	rewards reward.Calculator
}

func (b *builder) ResetAtomicUTXOManager(aum avax.AtomicUTXOManager) {
	b.AtomicUTXOManager = aum
}
