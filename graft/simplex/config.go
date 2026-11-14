// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"time"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
)

// DefaultWALGCInterval is the WALGCInterval used when none is configured.
const DefaultWALGCInterval uint64 = 10_000

type ParameterConfig struct {
	// WALMaxSizeBytes is the maximum size, in bytes, that a write-ahead log may reach
	// before it is closed and a new one is started. Zero selects the default.
	WALMaxSizeBytes int
	// MaxNetworkDelay is the assumed upper bound on the network delay for messages to be delivered.
	MaxNetworkDelay time.Duration
	// MaxRoundWindow is the maximum number of rounds that can be stored in memory.
	MaxRoundWindow uint64
	// WALGCInterval is the number of indexed blocks between WAL garbage collections.
	// Defaults to DefaultWALGCInterval.
	WALGCInterval uint64
	// MaxBlacklistedRounds is the maximum number of rounds a node may remain blacklisted for.
	// Changing it takes effect gradually, as each block may only move the value recorded in the chain by a single orbit.
	// Defaults to DefaultMaxBlacklistedRounds
	MaxBlacklistedRounds uint16
}

// PlatformChain is an interface that abstracts the interaction with the P-chain.
type PlatformChain interface {
	GetValidatorSet(uint64) (metadata.NodeBLSMappings, error)
	// GenesisValidatorSet returns the first ever validator set for this network.
	GenesisValidatorSet() metadata.NodeBLSMappings
	// GetMinimumHeight returns the minimum height of the block still in the proposal window.
	GetMinimumHeight() uint64
	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight() uint64
	// WaitForProgress should block until either the context is cancelled, or the P-chain height has increased from the provided pChainHeight.
	WaitForProgress(ctx context.Context, pChainHeight uint64) error
	// LastNonSimplexBlockPChainHeight returns the P-chain height of the last non-simplex block in the chain.
	LastNonSimplexBlockPChainHeight() uint64
}

type Broadcaster interface {
	Broadcast(msg *common.Message)
}

// Sender is an interface that defines the ability to send messages to other nodes in the network.
type Sender interface {
	// Send sends a message to the given destination node
	Send(msg *common.Message, destination common.NodeID)
}

type VM interface {
	// BuildBlock builds a block given the current context and the P-chain height.
	BuildBlock(ctx context.Context, pChainHeight uint64) (avalanchego.VMBlock, error)

	// WaitForPendingBlock returns when either the given context is cancelled,
	// or when the VM signals that a block should be built.
	WaitForPendingBlock(ctx context.Context)

	// ParseBlock parses the given block bytes into a VMBlock.
	ParseBlock(context.Context, []byte) (avalanchego.VMBlock, error)
}

type Storage interface {
	// GetBlock retrieves the block and finalization at [seq] with the given digest.
	// If the digest is nil, the block with the given sequence number is returned.
	// If [seq] the block cannot be found, returns ErrBlockNotFound.
	GetBlock(seq uint64) (metadata.StateMachineBlock, *common.Finalization, error)

	// NumBlocks returns the number of blocks stored in the storage.
	NumBlocks() uint64

	// Index indexes the given block and finalization in the storage.
	Index(ctx context.Context, block common.VerifiedBlock, certificate common.Finalization) error
}

type CryptoOps interface {
	// Sign signs the given message.
	Sign(message []byte) (common.SignatureBytes, error)
	// AggregateKeys combines the given public keys into a single aggregated public key.
	AggregateKeys(keys ...common.PublicKeyBytes) (common.PublicKeyBytes, error)
	// VerifySignature verifies that signature is a valid signature over message by the holder of publicKey.
	VerifySignature(message []byte, signature common.SignatureBytes, publicKey common.PublicKeyBytes) error
	// CreateSignatureAggregator creates a new signature aggregator for the given nodes.
	CreateSignatureAggregator([]common.Node) common.SignatureAggregator
	// DeserializeQuorumCertificate deserializes the given bytes into a QuorumCertificate.
	DeserializeQuorumCertificate(bytes []byte) (common.QuorumCertificate, error)
}
