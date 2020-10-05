package benchlist

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// Manager provides an interface for a benchlist to register whether
// queries have been successful or unsuccessful and place validators with
// consistently failing queries on a benchlist to prevent waiting up to
// the full network timeout for their responses.
type Manager interface {
	// RegisterQuery registers a sent query and returns whether the query is subject to benchlist
	RegisterQuery(ids.ID, ids.ShortID, uint32, constants.MsgType) bool
	// RegisterResponse registers the response to a query message
	RegisterResponse(ids.ID, ids.ShortID, uint32)
	// QueryFailed registers that a query did not receive a response within our synchrony bound
	QueryFailed(ids.ID, ids.ShortID, uint32)
	// RegisterChain registers a new chain with metrics under [namespac]
	RegisterChain(ids.ID, string)
}

// Config defines the configuration for a benchlist
type Config struct {
	Validators         validators.Manager
	Threshold          int
	Duration           time.Duration
	MaxPortion         float64
	PeerSummaryEnabled bool
}

type benchlistManager struct {
	config    *Config
	ctxLookup snow.ContextLookup
	// Chain ID --> benchlist for that chain
	chainBenchlists map[[32]byte]QueryBenchlist

	lock sync.RWMutex
}

// NewManager returns a manager for chain-specific query benchlisting
func NewManager(config *Config, ctxLookup snow.ContextLookup) Manager {
	return &benchlistManager{
		config:          config,
		ctxLookup:       ctxLookup,
		chainBenchlists: make(map[[32]byte]QueryBenchlist),
	}
}

func (bm *benchlistManager) RegisterChain(chainID ids.ID, namespace string) {
	bm.lock.Lock()
	defer bm.lock.Unlock()

	key := chainID.Key()
	chain, exists := bm.chainBenchlists[key]
	if exists {
		return
	}

	vdrs, ok := bm.config.Validators.GetValidatorsByChain(chainID)
	if !ok {
		return
	}
	ctx, exists := bm.ctxLookup.GetContext(chainID)
	if !exists {
		return
	}
	chain = NewQueryBenchlist(vdrs, ctx, bm.config.Threshold, bm.config.Duration, bm.config.MaxPortion, bm.config.PeerSummaryEnabled, namespace)
	bm.chainBenchlists[key] = chain
}

// RegisterQuery implements the Manager interface
func (bm *benchlistManager) RegisterQuery(chainID ids.ID, validatorID ids.ShortID, requestID uint32, msgType constants.MsgType) bool {
	bm.lock.RLock()
	defer bm.lock.RUnlock()

	key := chainID.Key()
	chain, exists := bm.chainBenchlists[key]
	if !exists {
		return false
	}

	return chain.RegisterQuery(validatorID, requestID, msgType)
}

// RegisterResponse implements the Manager interface
func (bm *benchlistManager) RegisterResponse(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {
	bm.lock.RLock()
	defer bm.lock.RUnlock()

	chain, exists := bm.chainBenchlists[chainID.Key()]
	if !exists {
		return
	}

	chain.RegisterResponse(validatorID, requestID)
}

// QueryFailed implements the Manager interface
func (bm *benchlistManager) QueryFailed(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {
	bm.lock.RLock()
	defer bm.lock.RUnlock()

	chain, exists := bm.chainBenchlists[chainID.Key()]
	if !exists {
		return
	}

	chain.QueryFailed(validatorID, requestID)
}

type noBenchlist struct{}

// NewNoBenchlist returns an empty benchlist that will never stop any queries
func NewNoBenchlist() Manager { return &noBenchlist{} }

// RegisterChain ...
func (b *noBenchlist) RegisterChain(chainID ids.ID, namespace string) {}

// RegisterQuery ...
func (b *noBenchlist) RegisterQuery(chainID ids.ID, validatorID ids.ShortID, requestID uint32, msgType constants.MsgType) bool {
	return true
}

// RegisterResponse ...
func (b *noBenchlist) RegisterResponse(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {}

// QueryFailed ...
func (b *noBenchlist) QueryFailed(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {}
