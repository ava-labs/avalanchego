// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"crypto"
	"crypto/x509"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type EventDispatcher interface {
	Issue(ctx *Context, containerID ids.ID, container []byte) error
	// If the returned error is non-nil, the chain associated with [ctx] should shut
	// down and not commit [container] or any other container to its database as accepted.
	// Accept must be called before [containerID] is committed to the VM as accepted.
	Accept(ctx *Context, containerID ids.ID, container []byte) error
	Reject(ctx *Context, containerID ids.ID, container []byte) error
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

	Log                 logging.Logger
	DecisionDispatcher  EventDispatcher
	ConsensusDispatcher EventDispatcher
	Lock                sync.RWMutex
	Keystore            keystore.BlockchainKeystore
	SharedMemory        atomic.SharedMemory
	BCLookup            ids.AliaserReader
	SNLookup            SubnetLookup
	Namespace           string
	Metrics             prometheus.Registerer

	// Epoch management
	EpochFirstTransition time.Time
	EpochDuration        time.Duration
	Clock                timer.Clock

	// Non-zero iff this chain bootstrapped.
	bootstrapped utils.AtomicBool

	// Indicates this chain is available to only validators.
	validatorOnly utils.AtomicBool

	// snowman++ attributes
	ValidatorState    validators.State  // interface for P-Chain validators
	StakingLeafSigner crypto.Signer     // block signer
	StakingCertLeaf   *x509.Certificate // block certificate
}

// IsBootstrapped returns true iff this chain is done bootstrapping
func (ctx *Context) IsBootstrapped() bool {
	return ctx.bootstrapped.GetValue()
}

// Bootstrapped marks this chain as done bootstrapping
func (ctx *Context) Bootstrapped() {
	ctx.bootstrapped.SetValue(true)
}

// IsValidatorOnly returns true iff this chain is available only to validators
func (ctx *Context) IsValidatorOnly() bool {
	return ctx.validatorOnly.GetValue()
}

// SetValidatorOnly  marks this chain as available only to validators
func (ctx *Context) SetValidatorOnly() {
	ctx.validatorOnly.SetValue(true)
}

// Epoch this context thinks it's in based on the wall clock time.
func (ctx *Context) Epoch() uint32 {
	now := ctx.Clock.Time()
	timeSinceFirstEpochTransition := now.Sub(ctx.EpochFirstTransition)
	epochsSinceFirstTransition := timeSinceFirstEpochTransition / ctx.EpochDuration
	currentEpoch := epochsSinceFirstTransition + 1
	if currentEpoch < 0 {
		return 0
	}
	return uint32(currentEpoch)
}

func DefaultContextTest() *Context {
	return &Context{
		NetworkID:           0,
		SubnetID:            ids.Empty,
		ChainID:             ids.Empty,
		NodeID:              ids.ShortEmpty,
		Log:                 logging.NoLog{},
		DecisionDispatcher:  emptyEventDispatcher{},
		ConsensusDispatcher: emptyEventDispatcher{},
		BCLookup:            ids.NewAliaser(),
		Namespace:           "",
		Metrics:             prometheus.NewRegistry(),
	}
}

type emptyEventDispatcher struct{}

func (emptyEventDispatcher) Issue(*Context, ids.ID, []byte) error  { return nil }
func (emptyEventDispatcher) Accept(*Context, ids.ID, []byte) error { return nil }
func (emptyEventDispatcher) Reject(*Context, ids.ID, []byte) error { return nil }
