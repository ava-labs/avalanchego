// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Factory = fakeFactory{}

// fakeFactory is a [vms.Factory] that returns a fixed VM.
type fakeFactory struct {
	vm *fakeVM
}

func (f fakeFactory) New(logging.Logger) (interface{}, error) {
	return f.vm, nil
}

// TestFactoryUsableBeforeInitialize verifies [VM.Version] and [VM.Shutdown]
// work on a factory-built VM before Initialize.
func TestFactoryUsableBeforeInitialize(t *testing.T) {
	f := &Factory{
		PreFactory:  fakeFactory{vm: newFakeVM(t, "pre", newFakeState())},
		PostFactory: fakeFactory{vm: newFakeVM(t, "post", newFakeState())},
	}

	intf, err := f.New(logging.NoLog{})
	require.NoError(t, err)
	vm := intf.(*VM)

	// New marks the pre-transition chain current, so these route to it.
	version, err := vm.Version(t.Context())
	require.NoError(t, err)
	require.Equal(t, "pre", version)

	require.NoError(t, vm.Shutdown(t.Context()))
}
