// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"go.uber.org/zap"
)

type Logger interface {
	// Log that a fatal error has occurred. The program should likely exit soon
	// after this is called
	Fatal(msg string, fields ...zap.Field)
	// Log that an error has occurred. The program should be able to recover
	// from this error
	Error(msg string, fields ...zap.Field)
	// Log that an event has occurred that may indicate a future error or
	// vulnerability
	Warn(msg string, fields ...zap.Field)
	// Log an event that may be useful for a user to see to measure the progress
	// of the protocol
	Info(msg string, fields ...zap.Field)
	// Log an event that may be useful for understanding the order of the
	// execution of the protocol
	Trace(msg string, fields ...zap.Field)
	// Log an event that may be useful for a programmer to see when debuging the
	// execution of the protocol
	Debug(msg string, fields ...zap.Field)
	// Log extremely detailed events that can be useful for inspecting every
	// aspect of the program
	Verbo(msg string, fields ...zap.Field)
}

var (
	// ErrShouldBuildEmptyBlock is returned when a block needs to be built even though the VM has no transactions to include in the block.
	// This is used to advance the chain when the tip is notarized but not finalized.
	ErrShouldBuildEmptyBlock = errors.New("should build empty block")

	// ErrBlockNotFound is returned when a block cannot be found in storage.
	ErrBlockNotFound = fmt.Errorf("block not found")
)

type BlockBuilder interface {
	// BuildBlock blocks until some transactions are available to be batched into a block,
	// and the given metadata and blacklist are encoded into the built block.
	// Returns a block and true unless the given context is cancelled by the caller.
	// When the given context is cancelled by the caller:
	// returns an empty block and true, if the context was cancelled with ErrShouldBuildEmptyBlock
	// returns nil, false otherwise.
	// The returned boolean indicates whether the block is nil or not.
	BuildBlock(ctx context.Context, metadata ProtocolMetadata, blacklist Blacklist) (VerifiedBlock, bool)

	// WaitForPendingBlock returns when either the given context is cancelled,
	// or when the application signals that a block should be built.
	WaitForPendingBlock(ctx context.Context)
}

type Storage interface {
	NumBlocks() uint64
	// Retrieve returns the block and finalization at [seq].
	// If [seq] the block cannot be found, returns ErrBlockNotFound.
	Retrieve(seq uint64) (VerifiedBlock, Finalization, error)
	Index(ctx context.Context, block VerifiedBlock, certificate Finalization) error
}

type Communication interface {
	// Validators returns all nodes that participate in consensus in the epoch.
	Validators() Nodes

	// Send sends a message to the given destination node
	Send(msg *Message, destination NodeID)

	// Broadcast broadcasts the given message to all nodes.
	// Does not send it to yourself.
	Broadcast(msg *Message)
}

type Signer interface {
	Sign(message []byte) (SignatureBytes, error)
}

// SignatureBytes is the byte representation of a signature, or of an aggregate of signatures.
type SignatureBytes []byte

// PublicKeyBytes is the byte representation of a public key, or of an aggregate of public keys.
type PublicKeyBytes []byte

// SignatureVerifier verifies that signature is a valid signature over message by the holder of publicKey.
type SignatureVerifier interface {
	VerifySignature(message []byte, signature SignatureBytes, publicKey PublicKeyBytes) error
}

type WriteAheadLog interface {
	Append([]byte) error
	ReadAll() ([][]byte, error)
	Close() error
}

type Block interface {
	// BlockHeader encodes a succinct and collision-free representation of a block.
	BlockHeader() BlockHeader

	Blacklist() Blacklist

	// Verify verifies the block by speculatively executing it on top of its ancestor.
	Verify(context.Context, ...VerifyOptions) (VerifiedBlock, error)

	// non nil only for sealing blocks & first ever simplex block
	SealingBlockInfo() *SealingBlockInfo
}

// SealingBlockInfo defines information that is derived from a sealing block,
// namely the validator set and the hash of the previous sealing block if applicable.
type SealingBlockInfo struct {
	// ValidatorSet of the new epoch
	ValidatorSet Nodes
	// PrevSealingBlockHash is the hash of the previous sealing block
	PrevSealingBlockHash Digest
}

func (s *SealingBlockInfo) String() string {
	return fmt.Sprintf("Info: Num Validators %d, PrevHash %s", len(s.ValidatorSet), s.PrevSealingBlockHash)
}

type VerifiedBlock interface {
	// BlockHeader encodes a succinct and collision-free representation of a block.
	BlockHeader() BlockHeader

	Blacklist() Blacklist

	// Bytes returns a byte encoding of the block
	Bytes() []byte

	// Size returns the number of bytes in the encoding of the block
	Size() int

	// SealingBlockInfo returns a non-nil value for a block that is not a sealing block and that is not the first ever simplex block.
	SealingBlockInfo() *SealingBlockInfo
}

// BlockDeserializer deserializes blocks according to formatting
// enforced by the application.
type BlockDeserializer interface {
	// DeserializeBlock deserializes the given bytes and initializes a VerifiedBlock.
	// Returns an error upon failure.
	DeserializeBlock(ctx context.Context, bytes []byte) (Block, error)
}

// Signature encodes a signature and the node that signed it, without the message it was signed on.
type Signature struct {
	// Signer is the NodeID of the creator of the signature.
	Signer NodeID
	// Value is the byte representation of the signature.
	Value SignatureBytes
}

// QCDeserializer deserializes QuorumCertificates according to formatting
type QCDeserializer interface {
	// DeserializeQuorumCertificate parses the given bytes and initializes a QuorumCertificate.
	// Returns an error upon failure.
	DeserializeQuorumCertificate(bytes []byte) (QuorumCertificate, error)
}

// SignatureAggregator aggregates signatures into a QuorumCertificate
type SignatureAggregator interface {
	// Aggregate aggregates several signatures into a QuorumCertificate
	Aggregate([]Signature) (QuorumCertificate, error)

	// AppendSignatures appends signatures to an existing signature.
	// If the existing signature is empty, it just aggregates the given signatures.
	AppendSignatures(SignatureBytes, ...SignatureBytes) (SignatureBytes, error)

	// IsQuorum returns true if the given signers constitute a quorum.
	// In the case of PoA, this means at least a quorum of the nodes are given.
	// In the case of PoS, this means at least two thirds of the st.
	IsQuorum([]NodeID) bool

	// AboveByzantineThreshold returns true if the given signers constitute a set that includes at least one non Byzantine node.
	AboveByzantineThreshold([]NodeID) bool
}

// Nodes is a list of Node elements.
type Nodes []Node

// NodeIDs returns the NodeIDs of the nodes in the Nodes.
func (nws Nodes) NodeIDs() []NodeID {
	nodes := make([]NodeID, len(nws))
	for i, nw := range nws {
		nodes[i] = nw.Id
	}
	return nodes
}

func (nws Nodes) Contains(nodeID NodeID) bool {
	for _, nw := range nws {
		if bytes.Equal(nw.Id, nodeID) {
			return true
		}
	}
	return false
}

// Equal returns whether both hold the same nodes, ignoring order.
func (nws Nodes) Equal(other Nodes) bool {
	if len(nws) != len(other) {
		return false
	}

	nwsClone := slices.Clone(nws)
	otherClone := slices.Clone(other)
	SortNodes(nwsClone)
	SortNodes(otherClone)

	for i := range nwsClone {
		if !bytes.Equal(nwsClone[i].Id, otherClone[i].Id) {
			return false
		}
		if !bytes.Equal(nwsClone[i].PK, otherClone[i].PK) {
			return false
		}
		if nwsClone[i].Weight != otherClone[i].Weight {
			return false
		}
	}
	return true
}

// Node is a struct that pairs a node ID with its weight and public key.
type Node struct {
	Id     NodeID
	Weight uint64
	PK     PublicKeyBytes
}

// SortNodes sorts the nodes in place by their byte representations.
func SortNodes(nodes Nodes) {
	slices.SortFunc(nodes, func(a, b Node) int {
		return bytes.Compare(a.Id[:], b.Id[:])
	})
}

func Quorum(n int) int {
	f := (n - 1) / 3
	// Obtained from the equation:
	// Quorum * 2 = N + F + 1
	return (n+f)/2 + 1
}

func F(n int) int {
	return (n - 1) / 3
}

// SignatureAggregatorCreator creates a SignatureAggregator from a list of nodes and their weights.
type SignatureAggregatorCreator func([]Node) SignatureAggregator

type VerifyOptions func(*VerifyConfig)

func OnlyVMVerifyOpt(vc *VerifyConfig) {
	vc.OnlyVM = true
}

type VerifyConfig struct {
	OnlyVM bool
}
