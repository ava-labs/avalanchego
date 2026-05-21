// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/MetalBlockchain/metalgo/database"
	"github.com/MetalBlockchain/metalgo/network/p2p"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/snow/engine/common"
	"github.com/MetalBlockchain/metalgo/snow/engine/common/tracker"
	"github.com/MetalBlockchain/metalgo/snow/engine/snowman/block"
	"github.com/MetalBlockchain/metalgo/snow/validators"
)

type Config struct {
	common.AllGetsServer

	Ctx     *snow.ConsensusContext
	Beacons validators.Manager

	SampleK          int
	StartupTracker   tracker.Startup
	Sender           common.Sender
	BootstrapTracker common.BootstrapTracker

	// PeerTracker manages the set of nodes that we fetch the next block from.
	PeerTracker *p2p.PeerTracker

	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	AncestorsMaxContainersReceived int

	// Database used to track the fetched, but not yet executed, blocks during
	// bootstrapping.
	DB database.Database

	VM block.ChainVM

	// NonVerifyingParse parses blocks without verifying them.
	NonVerifyingParse block.ParseFunc

	Bootstrapped func()

	common.Haltable
}
