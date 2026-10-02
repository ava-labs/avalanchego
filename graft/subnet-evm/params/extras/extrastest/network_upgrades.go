// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package extrastest provides test helpers for the Subnet-EVM params extras.
package extrastest

import "github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"

// SetSubnetEVMTimestamp sets the timestamp that activates every Avalanche
// upgrade prior to Durango, which Subnet-EVM activates at once.
func SetSubnetEVMTimestamp(n *extras.NetworkUpgrades, t *uint64) {
	n.ApricotPhase1BlockTimestamp = t
	n.ApricotPhase2BlockTimestamp = t
	n.ApricotPhase3BlockTimestamp = t
	n.ApricotPhase4BlockTimestamp = t
	n.ApricotPhase5BlockTimestamp = t
	n.ApricotPhasePre6BlockTimestamp = t
	n.ApricotPhase6BlockTimestamp = t
	n.ApricotPhasePost6BlockTimestamp = t
	n.BanffBlockTimestamp = t
	n.CortinaBlockTimestamp = t
}
