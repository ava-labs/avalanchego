package chains

import (
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/networking/router"
)

// MockManager implements Manager but does nothing. Always returns nil error.
// To be used only in tests
type MockManager struct{}

// Router ...
func (mm MockManager) Router() router.Router { return nil }

// CreateChain ...
func (mm MockManager) CreateChain(ChainParameters) {}

// ForceCreateChain ...
func (mm MockManager) ForceCreateChain(ChainParameters) {}

// AddRegistrant ...
func (mm MockManager) AddRegistrant(Registrant) {}

// Lookup ...
func (mm MockManager) Lookup(s string) (ids.ID, error) {
	id, err := ids.FromString(s)
	if err == nil {
		return id, nil
	}
	return ids.ID{}, nil
}

// LookupVM ...
func (mm MockManager) LookupVM(s string) (ids.ID, error) {
	id, err := ids.FromString(s)
	if err == nil {
		return id, nil
	}
	return ids.ID{}, nil
}

// Aliases ...
func (mm MockManager) Aliases(ids.ID) []string { return nil }

// Alias ...
func (mm MockManager) Alias(ids.ID, string) error { return nil }

// Shutdown ...
func (mm MockManager) Shutdown() {}

// SubnetID ...
func (mm MockManager) SubnetID(ids.ID) (ids.ID, error) { return ids.ID{}, nil }

// IsBootstrapped ...
func (mm MockManager) IsBootstrapped(ids.ID) bool { return false }
