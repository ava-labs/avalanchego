// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/featurextension"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	txstate "github.com/ava-labs/avalanchego/vms/platformvm/state/transactions"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	platformutils "github.com/ava-labs/avalanchego/vms/platformvm/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxos"
)

var _ TxVerifier = &verifier{}

// TxVerifier collects all stuff needed to validate transactions.
type TxVerifier interface {
	utxos.SpendHandler
	uptime.Manager
	reward.Calculator

	Clock() *mockable.Clock
	Ctx() *snow.Context
	PlatformConfig() *config.Config
	Bootstrapped() bool
	FeatureExtension() featurextension.Fx
	CreateChain(tx unsigned.Tx) error
}

func NewVerifier(
	ctx *snow.Context,
	bootstrapped *utils.AtomicBool,
	cfg *config.Config,
	clk *mockable.Clock,
	fx featurextension.Fx,
	state txstate.Mutable,
	timeMan uptime.Manager,
	utxosMan utxos.SpendHandler,
	rewards reward.Calculator,
) TxVerifier {
	return &verifier{
		bootstrapped: bootstrapped,
		Manager:      timeMan,
		SpendHandler: utxosMan,
		state:        state,
		cfg:          cfg,
		ctx:          ctx,
		clk:          clk,
		fx:           fx,
		Calculator:   rewards,
	}
}

type verifier struct {
	utxos.SpendHandler
	uptime.Manager
	reward.Calculator

	clk          *mockable.Clock
	ctx          *snow.Context
	cfg          *config.Config
	bootstrapped *utils.AtomicBool
	fx           featurextension.Fx

	state txstate.Mutable
}

func (v *verifier) Clock() *mockable.Clock               { return v.clk }
func (v *verifier) Ctx() *snow.Context                   { return v.ctx }
func (v *verifier) PlatformConfig() *config.Config       { return v.cfg }
func (v *verifier) Bootstrapped() bool                   { return v.bootstrapped.GetValue() }
func (v *verifier) FeatureExtension() featurextension.Fx { return v.fx }

func (v *verifier) CreateChain(tx unsigned.Tx) error {
	return platformutils.CreateChain(*v.cfg, tx)
}
