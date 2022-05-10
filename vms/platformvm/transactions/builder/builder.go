// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/featurextension"
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
	fx featurextension.Fx,
	state state.Mutable,
	atoUtxosMan avax.AtomicUTXOManager,
	timeMan uptime.Manager,
	utxosMan utxos.SpendHandler,
	rewards reward.Calculator,
) TxBuilder {
	return &builder{
		AtomicUTXOManager: atoUtxosMan,
		Manager:           timeMan,
		SpendHandler:      utxosMan,
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
	uptime.Manager
	utxos.SpendHandler
	state state.Mutable

	cfg     config.Config
	ctx     *snow.Context
	clk     mockable.Clock
	fx      featurextension.Fx
	rewards reward.Calculator
}

func (b *builder) ResetAtomicUTXOManager(aum avax.AtomicUTXOManager) {
	b.AtomicUTXOManager = aum
}
