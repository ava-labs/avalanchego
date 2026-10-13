// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
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

	// SkipCheckpoints, if non-nil and true when syncing starts, skips the
	// network's hard-coded checkpoints. Checkpoints defend against malicious
	// peers; a fork that has switched to its own validators trusts every peer,
	// and state-synced peers cannot serve ancient checkpoint blocks.
	SkipCheckpoints func() bool

	Bootstrapped func()

	common.Haltable
}
