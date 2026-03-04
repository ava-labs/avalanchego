// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import "github.com/ava-labs/avalanchego/ids"

// TestManager implements Manager but does nothing. Always returns nil error.
// To be used only in tests
var TestManager Manager = testManager{}

type testManager struct{}

func (testManager) QueueChainCreation(ChainParameters) {}

func (testManager) AddRegistrant(Registrant) {}

func (testManager) Aliases(ids.ID) ([]string, error) {
	return nil, nil
}

func (testManager) PrimaryAlias(ids.ID) (string, error) {
	return "", nil
}

func (testManager) PrimaryAliasOrDefault(ids.ID) string {
	return ""
}

func (testManager) Alias(ids.ID, string) error {
	return nil
}

func (testManager) RemoveAliases(ids.ID) {}

func (testManager) Shutdown() {}

func (testManager) StartChainCreator(ChainParameters) error {
	return nil
}

func (testManager) IsBootstrapped(ids.ID) bool {
	return false
}

func (testManager) Lookup(s string) (ids.ID, error) {
	return ids.FromString(s)
}

func (testManager) LookupVM(s string) (ids.ID, error) {
	return ids.FromString(s)
}
