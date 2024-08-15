// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fee"
)

// Struct collecting all the foundational parameters of the AVM
type Config struct {
	Upgrades upgrade.Config

	fee.StaticConfig
}
