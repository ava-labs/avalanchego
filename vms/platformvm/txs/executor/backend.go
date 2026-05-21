// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/snow/uptime"
	"github.com/MetalBlockchain/metalgo/utils"
	"github.com/MetalBlockchain/metalgo/utils/timer/mockable"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/config"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/fx"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/reward"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/utxo"
)

type Backend struct {
	Config       *config.Internal
	Ctx          *snow.Context
	Clk          *mockable.Clock
	Fx           fx.Fx
	FlowChecker  utxo.Verifier
	Uptimes      uptime.Calculator
	Rewards      reward.Calculator
	Bootstrapped *utils.Atomic[bool]
}
