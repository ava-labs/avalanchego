// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

// We require that latest upgrades have enum value >= all previous upgrades
const (
	PreApricot Upgrade = iota
	ApricotPhase3
	ApricotPhase5
	Banff
	Cortina
	Durango
	EUpgrade
)

type Upgrade uint8
