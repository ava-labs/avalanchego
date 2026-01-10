// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"

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

	Bootstrapped func()

	// RequestStateSyncRetry is an optional callback that the bootstrapper can call
	// to request transitioning back to state syncing. This enables runtime retry
	// when block sync is taking too long. Returns nil if retry was successful,
	// or an error if retry is not possible or failed.
	RequestStateSyncRetry func(ctx context.Context) error

	// CheckpointInterval is the number of blocks between checkpoints.
	// If 0, defaults to 100,000 blocks (~5 hours for most chains).
	// Set to a very large value (e.g., math.MaxUint64) to effectively disable checkpointing.
	CheckpointInterval uint64

	common.Haltable
}
