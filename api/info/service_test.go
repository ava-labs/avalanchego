// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var errTest = errors.New("non-nil error")

type testFactory struct{}

func (testFactory) New(logging.Logger) (any, error) { return nil, nil }

type getVMsTest struct {
	info      *Info
	vmManager *vms.Manager
}

func initGetVMsTest(*testing.T) *getVMsTest {
	vmManager := vms.NewManager(logging.NoLog{}, ids.NewAliaser())
	return &getVMsTest{
		info: &Info{
			Parameters: Parameters{
				VMManager: vmManager,
			},
			log: logging.NoLog{},
		},
		vmManager: vmManager,
	}
}

// Tests GetVMs in the happy-case
func TestGetVMsSuccess(t *testing.T) {
	require := require.New(t)

	resources := initGetVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	// Register factories and aliases
	require.NoError(resources.vmManager.RegisterFactory(t.Context(), id1, testFactory{}))
	require.NoError(resources.vmManager.Alias(id1, "vm1-alias-1"))
	require.NoError(resources.vmManager.Alias(id1, "vm1-alias-2"))
	require.NoError(resources.vmManager.RegisterFactory(t.Context(), id2, testFactory{}))
	require.NoError(resources.vmManager.Alias(id2, "vm2-alias-1"))
	require.NoError(resources.vmManager.Alias(id2, "vm2-alias-2"))

	// we expect that we dedup the redundant alias of vmId.
	expectedVMRegistry := map[ids.ID][]string{
		id1: {"vm1-alias-1", "vm1-alias-2"},
		id2: {"vm2-alias-1", "vm2-alias-2"},
	}

	reply := GetVMsReply{}
	require.NoError(resources.info.GetVMs(nil, nil, &reply))
	require.Equal(expectedVMRegistry, reply.VMs)
}

// failingAliaser wraps an Aliaser and returns an error from Aliases.
type failingAliaser struct {
	ids.Aliaser
	err error
}

func (f *failingAliaser) Aliases(ids.ID) ([]string, error) {
	return nil, f.err
}

// Tests GetVMs if we can't get our vm aliases.
func TestGetVMsGetAliasesFails(t *testing.T) {
	resources := initGetVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	require.NoError(t, resources.vmManager.RegisterFactory(t.Context(), id1, testFactory{}))
	require.NoError(t, resources.vmManager.RegisterFactory(t.Context(), id2, testFactory{}))

	// Inject a failing aliaser so Aliases returns an error
	resources.vmManager.Aliaser = &failingAliaser{
		Aliaser: resources.vmManager.Aliaser,
		err:     errTest,
	}

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)
	require.ErrorIs(t, err, errTest)
}
