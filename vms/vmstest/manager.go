// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmstest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Manager = (*Manager)(nil)

type Manager struct {
	T *testing.T

	CantLookup              bool
	CantPrimaryAlias        bool
	CantPrimaryAliasOrDefault bool
	CantAliases             bool
	CantAlias               bool
	CantRemoveAliases       bool
	CantGetFactory          bool
	CantRegisterFactory     bool
	CantListFactories       bool
	CantVersions            bool

	LookupF              func(alias string) (ids.ID, error)
	PrimaryAliasF        func(id ids.ID) (string, error)
	PrimaryAliasOrDefaultF func(id ids.ID) string
	AliasesF             func(id ids.ID) ([]string, error)
	AliasF               func(id ids.ID, alias string) error
	RemoveAliasesF       func(id ids.ID)
	GetFactoryF          func(vmID ids.ID) (vms.Factory, error)
	RegisterFactoryF     func(ctx context.Context, vmID ids.ID, factory vms.Factory) error
	ListFactoriesF       func() ([]ids.ID, error)
	VersionsF            func() (map[string]string, error)
}

func (m *Manager) Lookup(alias string) (ids.ID, error) {
	if m.LookupF != nil {
		return m.LookupF(alias)
	}
	if m.CantLookup && m.T != nil {
		require.FailNow(m.T, "unexpectedly called Lookup")
	}
	return ids.Empty, nil
}

func (m *Manager) PrimaryAlias(id ids.ID) (string, error) {
	if m.PrimaryAliasF != nil {
		return m.PrimaryAliasF(id)
	}
	if m.CantPrimaryAlias && m.T != nil {
		require.FailNow(m.T, "unexpectedly called PrimaryAlias")
	}
	return "", nil
}

func (m *Manager) PrimaryAliasOrDefault(id ids.ID) string {
	if m.PrimaryAliasOrDefaultF != nil {
		return m.PrimaryAliasOrDefaultF(id)
	}
	if m.CantPrimaryAliasOrDefault && m.T != nil {
		require.FailNow(m.T, "unexpectedly called PrimaryAliasOrDefault")
	}
	return ""
}

func (m *Manager) Aliases(id ids.ID) ([]string, error) {
	if m.AliasesF != nil {
		return m.AliasesF(id)
	}
	if m.CantAliases && m.T != nil {
		require.FailNow(m.T, "unexpectedly called Aliases")
	}
	return nil, nil
}

func (m *Manager) Alias(id ids.ID, alias string) error {
	if m.AliasF != nil {
		return m.AliasF(id, alias)
	}
	if m.CantAlias && m.T != nil {
		require.FailNow(m.T, "unexpectedly called Alias")
	}
	return nil
}

func (m *Manager) RemoveAliases(id ids.ID) {
	if m.RemoveAliasesF != nil {
		m.RemoveAliasesF(id)
		return
	}
	if m.CantRemoveAliases && m.T != nil {
		require.FailNow(m.T, "unexpectedly called RemoveAliases")
	}
}

func (m *Manager) GetFactory(vmID ids.ID) (vms.Factory, error) {
	if m.GetFactoryF != nil {
		return m.GetFactoryF(vmID)
	}
	if m.CantGetFactory && m.T != nil {
		require.FailNow(m.T, "unexpectedly called GetFactory")
	}
	return nil, nil
}

func (m *Manager) RegisterFactory(ctx context.Context, vmID ids.ID, factory vms.Factory) error {
	if m.RegisterFactoryF != nil {
		return m.RegisterFactoryF(ctx, vmID, factory)
	}
	if m.CantRegisterFactory && m.T != nil {
		require.FailNow(m.T, "unexpectedly called RegisterFactory")
	}
	return nil
}

func (m *Manager) ListFactories() ([]ids.ID, error) {
	if m.ListFactoriesF != nil {
		return m.ListFactoriesF()
	}
	if m.CantListFactories && m.T != nil {
		require.FailNow(m.T, "unexpectedly called ListFactories")
	}
	return nil, nil
}

func (m *Manager) Versions() (map[string]string, error) {
	if m.VersionsF != nil {
		return m.VersionsF()
	}
	if m.CantVersions && m.T != nil {
		require.FailNow(m.T, "unexpectedly called Versions")
	}
	return nil, nil
}
