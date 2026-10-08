// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// stubContract is a comparable [libevm.PrecompiledContract] whose required
// gas is its id, so distinct stubs are distinguishable by behaviour.
type stubContract struct{ id uint64 }

func (c stubContract) RequiredGas([]byte) uint64 { return c.id }
func (stubContract) Run([]byte) ([]byte, error)  { return nil, nil }

// stubPredicater satisfies [precompileconfig.Predicater] without behaviour.
type stubPredicater struct{}

func (stubPredicater) PredicateGas(predicate.Predicate, precompileconfig.Rules) (uint64, error) {
	return 0, nil
}

func (stubPredicater) VerifyPredicate(*precompileconfig.PredicateContext, predicate.Predicate) error {
	return nil
}

var (
	addrA = common.Address{0xa}
	addrB = common.Address{0xb}
	addrC = common.Address{0xc}
)

func validSet() *Set {
	return &Set{
		Contracts: map[common.Address]libevm.PrecompiledContract{
			addrA: stubContract{1},
			addrB: stubContract{2},
		},
		Predicaters: map[common.Address]precompileconfig.Predicater{
			addrA: stubPredicater{},
		},
		Active: []common.Address{addrB},
	}
}

func TestSetContract(t *testing.T) {
	s := validSet()

	got, ok := s.Contract(addrA)
	require.True(t, ok, "Contract(known address)")
	require.Equal(t, stubContract{1}, got, "Contract(known address)")
	require.Equal(t, uint64(1), got.RequiredGas(nil), "Contract(known address).RequiredGas()")

	_, ok = s.Contract(addrC)
	require.False(t, ok, "Contract(unknown address)")
}

func TestSetHasPredicate(t *testing.T) {
	s := validSet()
	require.True(t, s.HasPredicate(addrA), "HasPredicate(predicater address)")
	require.False(t, s.HasPredicate(addrB), "HasPredicate(contract without predicater)")
	require.False(t, s.HasPredicate(addrC), "HasPredicate(unknown address)")

	var _ predicate.Predicates = s
}

func TestSetVerify(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Set)
		wantErr error
	}{
		{name: "valid", modify: func(*Set) {}},
		{
			name:    "nil_contract",
			modify:  func(s *Set) { s.Contracts[addrC] = nil },
			wantErr: ErrNilContract,
		},
		{
			name:    "predicater_without_contract",
			modify:  func(s *Set) { s.Predicaters[addrC] = stubPredicater{} },
			wantErr: ErrPredicaterWithoutContract,
		},
		{
			name:    "active_without_contract",
			modify:  func(s *Set) { s.Active = append(s.Active, addrC) },
			wantErr: ErrActiveWithoutContract,
		},
		{
			name:    "duplicate_active",
			modify:  func(s *Set) { s.Active = append(s.Active, addrB) },
			wantErr: ErrDuplicateActive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSet()
			tt.modify(s)
			require.ErrorIs(t, s.Verify(), tt.wantErr, "Verify()")
		})
	}
}
