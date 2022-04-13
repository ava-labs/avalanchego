// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/networking/router"
)

var _ Manager = MockManager{}

// MockManager implements Manager but does nothing. Always returns nil error.
// To be used only in tests
type MockManager struct{}

func (mm MockManager) Router() router.Router               { return nil }
func (mm MockManager) CreateChain(ChainParameters)         {}
func (mm MockManager) ForceCreateChain(ChainParameters)    {}
func (mm MockManager) AddRegistrant(Registrant)            {}
func (mm MockManager) Aliases(ids.ID) ([]string, error)    { return nil, nil }
func (mm MockManager) PrimaryAlias(ids.ID) (string, error) { return "", nil }
func (mm MockManager) PrimaryAliasOrDefault(ids.ID) string { return "" }
func (mm MockManager) Alias(ids.ID, string) error          { return nil }
func (mm MockManager) RemoveAliases(ids.ID)                {}
func (mm MockManager) Shutdown()                           {}
func (mm MockManager) SubnetID(ids.ID) (ids.ID, error)     { return ids.ID{}, nil }
func (mm MockManager) IsBootstrapped(ids.ID) bool          { return false }

func (mm MockManager) Lookup(s string) (ids.ID, error) {
	id, err := ids.FromString(s)
	if err == nil {
		return id, nil
	}
	return ids.ID{}, nil
}

func (mm MockManager) LookupVM(s string) (ids.ID, error) {
	id, err := ids.FromString(s)
	if err == nil {
		return id, nil
	}
	return ids.ID{}, nil
}
