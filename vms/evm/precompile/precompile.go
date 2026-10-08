// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package precompile defines how an EVM implementation supplies its stateful
// precompiled contracts to libevm, independently of coreth.
package precompile

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

var (
	ErrNilContract               = errors.New("nil precompile contract")
	ErrPredicaterWithoutContract = errors.New("predicater address has no contract")
	ErrActiveWithoutContract     = errors.New("active address has no contract")
	ErrDuplicateActive           = errors.New("duplicate active address")
)

var _ predicate.Predicates = (*Set)(nil)

// Set is the fixed collection of stateful precompiles active on a chain.
type Set struct {
	// Contracts maps each precompile address to its libevm contract.
	Contracts map[common.Address]libevm.PrecompiledContract
	// Predicaters maps the subset of addresses whose access-list entries are
	// predicates to their verifier. Every key MUST also be a key of
	// Contracts.
	Predicaters map[common.Address]precompileconfig.Predicater
	// Active lists the addresses reported to the EVM as active precompiles,
	// which the EVM pre-warms for EIP-2929 access costs. This is
	// consensus-visible and MAY be a strict subset of Contracts: coreth never
	// reported its module-registry precompiles, such as warp, as active.
	Active []common.Address
}

// Contract returns the contract at addr, if any.
func (s *Set) Contract(addr common.Address) (libevm.PrecompiledContract, bool) {
	c, ok := s.Contracts[addr]
	return c, ok
}

// HasPredicate implements [predicate.Predicates].
func (s *Set) HasPredicate(addr common.Address) bool {
	_, ok := s.Predicaters[addr]
	return ok
}

// Verify returns an error if the set is internally inconsistent.
func (s *Set) Verify() error {
	for addr, c := range s.Contracts {
		if c == nil {
			return fmt.Errorf("%w: %s", ErrNilContract, addr)
		}
	}
	for addr := range s.Predicaters {
		if _, ok := s.Contracts[addr]; !ok {
			return fmt.Errorf("%w: %s", ErrPredicaterWithoutContract, addr)
		}
	}
	seen := set.NewSet[common.Address](len(s.Active))
	for _, addr := range s.Active {
		if _, ok := s.Contracts[addr]; !ok {
			return fmt.Errorf("%w: %s", ErrActiveWithoutContract, addr)
		}
		if seen.Contains(addr) {
			return fmt.Errorf("%w: %s", ErrDuplicateActive, addr)
		}
		seen.Add(addr)
	}
	return nil
}

// PredicateReader is implemented by an EVM state reader that exposes the
// current transaction's access-list predicates. A contract obtains it by
// type-asserting [vm.PrecompileEnvironment.ReadOnlyState].
type PredicateReader interface {
	GetPredicate(address common.Address, index int) (predicate.Predicate, bool)
}
