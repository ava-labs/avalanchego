// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extension

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/eth"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/ava-labs/coreth/sync/handlers"

	"github.com/ava-labs/libevm/core/types"
)

var (
	errNilConfig              = errors.New("nil extension config")
	errNilSyncSummaryProvider = errors.New("nil sync summary provider")
	errNilSyncableParser      = errors.New("nil syncable parser")
)

type ExtensibleVM interface {
	// SetExtensionConfig sets the configuration for the VM extension
	// Should be called before any other method and only once
	SetExtensionConfig(config *Config) error

	// SetLastAcceptedBlock sets the last accepted block
	SetLastAcceptedBlock(lastAcceptedBlock snowman.Block) error
	// GetExtendedBlock returns the VMBlock for the given ID or an error if the block is not found
	GetExtendedBlock(context.Context, ids.ID) (ExtendedBlock, error)
	// LastAcceptedExtendedBlock returns the last accepted VM block
	LastAcceptedExtendedBlock() ExtendedBlock
	// IsBootstrapped returns true if the VM is bootstrapped
	IsBootstrapped() bool
	// Ethereum returns the Ethereum client
	Ethereum() *eth.Ethereum
	// Config returns the configuration for the VM
	Config() config.Config
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
	return nil
}
