// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

type testFactory struct{}

func (testFactory) New(logging.Logger) (interface{}, error) { return nil, nil }

type getVMsTest struct {
	info      *Info
	vmManager *vms.Manager
}

func initGetVMsTest(_ *testing.T) *getVMsTest {
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
