// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"crypto"
	"crypto/x509"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type EventDispatcher interface {
	Issuer
	// If the returned error is non-nil, the chain associated with [ctx] should shut
	// down and not commit [container] or any other container to its database as accepted.
	// Accept must be called before [containerID] is committed to the VM as accepted.
	Acceptor
	Rejector
}

type SubnetLookup interface {
	SubnetID(chainID ids.ID) (ids.ID, error)
}

// ContextInitializable represents an object that can be initialized
// given a *Context object
type ContextInitializable interface {
	// InitCtx initializes an object provided a *Context object
	InitCtx(ctx *Context)
}

// Context is information about the current execution.
// [NetworkID] is the ID of the network this context exists within.
// [ChainID] is the ID of the chain this context exists within.
// [NodeID] is the ID of this node
type Context struct {
	NetworkID uint32
	SubnetID  ids.ID
	ChainID   ids.ID
	NodeID    ids.ShortID

	XChainID    ids.ID
	AVAXAssetID ids.ID

	Log          logging.Logger
	Lock         sync.RWMutex
	Keystore     keystore.BlockchainKeystore
	SharedMemory atomic.SharedMemory
	BCLookup     ids.AliaserReader
	SNLookup     SubnetLookup
	Metrics      metrics.OptionalGatherer

	// snowman++ attributes
	ValidatorState    validators.State  // interface for P-Chain validators
	StakingLeafSigner crypto.Signer     // block signer
	StakingCertLeaf   *x509.Certificate // block certificate
}

type ConsensusContext struct {
	*Context

	Registerer prometheus.Registerer

	DecisionDispatcher  EventDispatcher
	ConsensusDispatcher EventDispatcher

	// Non-zero iff this chain bootstrapped.
	state utils.AtomicInterface

	// Non-zero iff this chain is executing transactions.
	executing utils.AtomicBool

	// Indicates this chain is available to only validators.
	validatorOnly utils.AtomicBool
}

func (ctx *ConsensusContext) SetState(newState State) {
	ctx.state.SetValue(newState)
}

func (ctx *ConsensusContext) GetState() State {
	stateInf := ctx.state.GetValue()
	return stateInf.(State)
}

// IsExecuting returns true iff this chain is still executing transactions.
func (ctx *ConsensusContext) IsExecuting() bool {
	return ctx.executing.GetValue()
}

// Executing marks this chain as executing or not.
// Set to "true" if there's an ongoing transaction.
func (ctx *ConsensusContext) Executing(b bool) {
	ctx.executing.SetValue(b)
}

// IsValidatorOnly returns true iff this chain is available only to validators
func (ctx *ConsensusContext) IsValidatorOnly() bool {
	return ctx.validatorOnly.GetValue()
}

// SetValidatorOnly  marks this chain as available only to validators
func (ctx *ConsensusContext) SetValidatorOnly() {
	ctx.validatorOnly.SetValue(true)
}

func DefaultContextTest() *Context {
	return &Context{
		NetworkID: 0,
		SubnetID:  ids.Empty,
		ChainID:   ids.Empty,
		NodeID:    ids.ShortEmpty,
		Log:       logging.NoLog{},
		BCLookup:  ids.NewAliaser(),
		Metrics:   metrics.NewOptionalGatherer(),
	}
}

func DefaultConsensusContextTest() *ConsensusContext {
	return &ConsensusContext{
		Context:             DefaultContextTest(),
		Registerer:          prometheus.NewRegistry(),
		DecisionDispatcher:  noOpEventDispatcher{},
		ConsensusDispatcher: noOpEventDispatcher{},
	}
}

type noOpEventDispatcher struct{}

func (noOpEventDispatcher) Issue(*ConsensusContext, ids.ID, []byte) error  { return nil }
func (noOpEventDispatcher) Accept(*ConsensusContext, ids.ID, []byte) error { return nil }
func (noOpEventDispatcher) Reject(*ConsensusContext, ids.ID, []byte) error { return nil }

var _ EventDispatcher = &EventDispatcherTracker{}

func NewEventDispatcherTracker() *EventDispatcherTracker {
	return &EventDispatcherTracker{
		issued:   make(map[ids.ID]int),
		accepted: make(map[ids.ID]int),
		rejected: make(map[ids.ID]int),
	}
}

// EventDispatcherTracker tracks the dispatched events by its ID and counts.
// Useful for testing.
type EventDispatcherTracker struct {
	mu sync.RWMutex
	// maps "issued" ID to its count
	issued map[ids.ID]int
	// maps "accepted" ID to its count
	accepted map[ids.ID]int
	// maps "rejected" ID to its count
	rejected map[ids.ID]int
}

func (evd *EventDispatcherTracker) IsIssued(containerID ids.ID) (int, bool) {
	evd.mu.RLock()
	cnt, ok := evd.issued[containerID]
	evd.mu.RUnlock()
	return cnt, ok
}

func (evd *EventDispatcherTracker) Issue(ctx *ConsensusContext, containerID ids.ID, container []byte) error {
	evd.mu.Lock()
	evd.issued[containerID]++
	evd.mu.Unlock()
	return nil
}

func (evd *EventDispatcherTracker) Accept(ctx *ConsensusContext, containerID ids.ID, container []byte) error {
	evd.mu.Lock()
	evd.accepted[containerID]++
	evd.mu.Unlock()
	return nil
}

func (evd *EventDispatcherTracker) IsAccepted(containerID ids.ID) (int, bool) {
	evd.mu.RLock()
	cnt, ok := evd.accepted[containerID]
	evd.mu.RUnlock()
	return cnt, ok
}

func (evd *EventDispatcherTracker) Reject(ctx *ConsensusContext, containerID ids.ID, container []byte) error {
	evd.mu.Lock()
	evd.rejected[containerID]++
	evd.mu.Unlock()
	return nil
}

func (evd *EventDispatcherTracker) IsRejected(containerID ids.ID) (int, bool) {
	evd.mu.RLock()
	cnt, ok := evd.rejected[containerID]
	evd.mu.RUnlock()
	return cnt, ok
}
