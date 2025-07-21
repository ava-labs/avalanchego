// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/subnet-evm/core/txpool/legacypool"
	"github.com/ava-labs/subnet-evm/plugin/evm/config"
)

// defaultTxPoolConfig uses [legacypool.DefaultConfig] to make a [config.TxPoolConfig]
// that can be passed to [config.Config.SetDefaults].
var defaultTxPoolConfig = config.TxPoolConfig{
	PriceLimit:   legacypool.DefaultConfig.PriceLimit,
	PriceBump:    legacypool.DefaultConfig.PriceBump,
	AccountSlots: legacypool.DefaultConfig.AccountSlots,
	GlobalSlots:  legacypool.DefaultConfig.GlobalSlots,
	AccountQueue: legacypool.DefaultConfig.AccountQueue,
	GlobalQueue:  legacypool.DefaultConfig.GlobalQueue,
	Lifetime:     legacypool.DefaultConfig.Lifetime,
}
