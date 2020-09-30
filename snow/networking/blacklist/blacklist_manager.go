package blacklist

import "github.com/ava-labs/avalanchego/ids"

// Manager provides an interface for a blacklist to register whether
// queries have been successful or unsuccessful and place validators with
// consistently failing queries on a blacklist to prevent waiting up to
// the full network timeout for their responses.
type Manager interface {
	// RegisterQuery registers a sent query and returns whether the query is subject to blacklist
	RegisterQuery(ids.ID, ids.ShortID, uint32) bool
	// RegisterResponse registers the response to a query message
	RegisterResponse(ids.ID, ids.ShortID, uint32)
	// QueryFailed registers that a query did not receive a response within our synchrony bound
	QueryFailed(ids.ID, ids.ShortID, uint32)
}

type blacklistManager struct {
	config *Config
	// Chain ID --> blacklist for that chain
	chainBlacklists map[[32]byte]QueryBlacklist
}

// NewManager returns a manager for chain-specific query blacklisting
func NewManager(config *Config) Manager {
	return &blacklistManager{
		config:          config,
		chainBlacklists: make(map[[32]byte]QueryBlacklist),
	}
}

// RegisterQuery implements the Manager interface
func (bm *blacklistManager) RegisterQuery(chainID ids.ID, validatorID ids.ShortID, requestID uint32) bool {
	key := chainID.Key()
	chain, exists := bm.chainBlacklists[key]
	if !exists {
		vdrs, ok := bm.config.Validators.GetValidatorsByChain(chainID)
		if !ok {
			return false
		}
		chain = NewQueryBlacklist(vdrs, bm.config.Threshold, bm.config.Duration, bm.config.MaxPortion)
		bm.chainBlacklists[key] = chain
	}

	return chain.RegisterQuery(validatorID, requestID)
}

// RegisterResponse implements the Manager interface
func (bm *blacklistManager) RegisterResponse(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {
	chain, exists := bm.chainBlacklists[chainID.Key()]
	if !exists {
		return
	}

	chain.RegisterResponse(validatorID, requestID)
}

// QueryFailed implements the Manager interface
func (bm *blacklistManager) QueryFailed(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {
	chain, exists := bm.chainBlacklists[chainID.Key()]
	if !exists {
		return
	}

	chain.QueryFailed(validatorID, requestID)
}

type noBlacklist struct{}

// NewNoBlacklist returns an empty blacklist that will never stop any queries
func NewNoBlacklist() Manager { return &noBlacklist{} }

// RegisterQuery ...
func (b *noBlacklist) RegisterQuery(chainID ids.ID, validatorID ids.ShortID, requestID uint32) bool {
	return true
}

// RegisterResponse ...
func (b *noBlacklist) RegisterResponse(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {}

// QueryFailed ...
func (b *noBlacklist) QueryFailed(chainID ids.ID, validatorID ids.ShortID, requestID uint32) {}
