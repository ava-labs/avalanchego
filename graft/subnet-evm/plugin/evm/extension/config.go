// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extension

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/config"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/plugin/evm/sync"
	"github.com/ava-labs/subnet-evm/sync/handlers"

	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	errNilConfig              = errors.New("nil extension config")
	errNilSyncSummaryProvider = errors.New("nil sync summary provider")
	errNilSyncableParser      = errors.New("nil syncable parser")
	errNilClock               = errors.New("nil clock")
)

type ExtensibleVM interface {
	// SetExtensionConfig sets the configuration for the VM extension
	// Should be called before any other method and only once
	SetExtensionConfig(config *Config) error
	// NewClient returns a client to send messages with for the given protocol
	NewClient(protocol uint64) *p2p.Client
	// AddHandler registers a server handler for an application protocol
	AddHandler(protocol uint64, handler p2p.Handler) error
	// GetExtendedBlock returns the VMBlock for the given ID or an error if the block is not found
	GetExtendedBlock(context.Context, ids.ID) (ExtendedBlock, error)
	// LastAcceptedExtendedBlock returns the last accepted VM block
	LastAcceptedExtendedBlock() ExtendedBlock
	// ChainConfig returns the chain config for the VM
	ChainConfig() *params.ChainConfig
	// P2PValidators returns the validators for the network
	P2PValidators() *p2p.Validators
	// Blockchain returns the blockchain client
	Blockchain() *core.BlockChain
	// Config returns the configuration for the VM
	Config() config.Config
	// MetricRegistry returns the metric registry for the VM
	MetricRegistry() *prometheus.Registry
	// ReadLastAccepted returns the last accepted block hash and height
	ReadLastAccepted() (common.Hash, uint64, error)
	// VersionDB returns the versioned database for the VM
	VersionDB() *versiondb.Database
}

// InnerVM is the interface that must be implemented by the VM
// that's being wrapped by the extension
type InnerVM interface {
	ExtensibleVM
	avalanchecommon.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM
}

// ExtendedBlock is a block that can be used by the extension
type ExtendedBlock interface {
	snowman.Block
	GetEthBlock() *types.Block
	GetBlockExtension() BlockExtension
}

type BlockExtender interface {
	// NewBlockExtension is called when a new block is created
	NewBlockExtension(b ExtendedBlock) (BlockExtension, error)
}

// BlockExtension allows the VM extension to handle block processing events.
type BlockExtension interface {
	// SyntacticVerify verifies the block syntactically
	// it can be implemented to extend inner block verification
	SyntacticVerify(rules extras.Rules) error
	// SemanticVerify verifies the block semantically
	// it can be implemented to extend inner block verification
	SemanticVerify() error
	// CleanupVerified is called when a block has passed SemanticVerify and SynctacticVerify,
	// and should be cleaned up due to error or verification runs under non-write mode. This
	// does not return an error because the block has already been verified.
	CleanupVerified()
	// Accept is called when a block is accepted by the block manager. Accept takes a
	// database.Batch that contains the changes that were made to the database as a result
	// of accepting the block. The changes in the batch should be flushed to the database in this method.
	Accept(acceptedBatch database.Batch) error
	// Reject is called when a block is rejected by the block manager
	Reject() error
}

// BuilderMempool is a mempool that's used in the block builder
type BuilderMempool interface {
	// PendingLen returns the number of pending transactions
	// that are waiting to be included in a block
	PendingLen() int
	// SubscribePendingTxs returns a channel that's signaled when there are pending transactions
	SubscribePendingTxs() <-chan struct{}
}

// LeafRequestConfig is the configuration to handle leaf requests
// in the network and syncer
type LeafRequestConfig struct {
	// LeafType is the type of the leaf node
	LeafType message.NodeType
	// MetricName is the name of the metric to use for the leaf request
	MetricName string
	// Handler is the handler to use for the leaf request
	Handler handlers.LeafRequestHandler
}

// Config is the configuration for the VM extension
type Config struct {
	// ConsensusCallbacks is the consensus callbacks to use
	// for the VM to be used in consensus engine.
	// Callback functions can be nil.
	ConsensusCallbacks dummy.ConsensusCallbacks
	// SyncSummaryProvider is the sync summary provider to use
	// for the VM to be used in syncer.
	// It's required and should be non-nil
	SyncSummaryProvider sync.SummaryProvider
	// SyncExtender can extend the syncer to handle custom sync logic.
	// It's optional and can be nil
	SyncExtender sync.Extender
	// SyncableParser is to parse summary messages from the network.
	// It's required and should be non-nil
	SyncableParser message.SyncableParser
	// BlockExtender allows the VM extension to create an extension to handle block processing events.
	// It's optional and can be nil
	BlockExtender BlockExtender
	// ExtraSyncLeafHandlerConfig is the extra configuration to handle leaf requests
	// in the network and syncer. It's optional and can be nil
	ExtraSyncLeafHandlerConfig *LeafRequestConfig
	// ExtraMempool is the mempool to be used in the block builder.
	// It's optional and can be nil
	ExtraMempool BuilderMempool
	// Clock is the clock to use for time related operations.
	// It's optional and can be nil
	Clock *mockable.Clock
}

func (c *Config) Validate() error {
	if c == nil {
		return errNilConfig
	}
	if c.SyncSummaryProvider == nil {
		return errNilSyncSummaryProvider
	}
	if c.SyncableParser == nil {
		return errNilSyncableParser
	}
	if c.Clock == nil {
		return errNilClock
	}
	return nil
}
