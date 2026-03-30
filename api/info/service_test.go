// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/vmstest"
)

var errTest = errors.New("non-nil error")

type getVMsTest struct {
	info          *Info
	mockVMManager *vmstest.Manager
}

func initGetVMsTest(t *testing.T) *getVMsTest {
	mockVMManager := &vmstest.Manager{}
	return &getVMsTest{
		info: &Info{
			Parameters: Parameters{
				VMManager: mockVMManager,
			},
			log: logging.NoLog{},
		},
		mockVMManager: mockVMManager,
	}
}

// Tests GetVMs in the happy-case
func TestGetVMsSuccess(t *testing.T) {
	require := require.New(t)

	resources := initGetVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	vmIDs := []ids.ID{id1, id2}
	// every vm is at least aliased to itself.
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}
	alias2 := []string{id2.String(), "vm2-alias-1", "vm2-alias-2"}
	// we expect that we dedup the redundant alias of vmId.
	expectedVMRegistry := map[ids.ID][]string{
		id1: alias1[1:],
		id2: alias2[1:],
	}

	resources.mockVMManager.ListFactoriesF = func() ([]ids.ID, error) {
		return vmIDs, nil
	}
	resources.mockVMManager.AliasesF = func(id ids.ID) ([]string, error) {
		switch id {
		case id1:
			return alias1, nil
		case id2:
			return alias2, nil
		default:
			return nil, errTest
		}
	}

	reply := GetVMsReply{}
	require.NoError(resources.info.GetVMs(nil, nil, &reply))
	require.Equal(expectedVMRegistry, reply.VMs)
}

// Tests GetVMs if we fail to list our vms.
func TestGetVMsVMsListFactoriesFails(t *testing.T) {
	resources := initGetVMsTest(t)

	resources.mockVMManager.ListFactoriesF = func() ([]ids.ID, error) {
		return nil, errTest
	}

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)
	require.ErrorIs(t, err, errTest)
}

// Tests GetVMs if we can't get our vm aliases.
func TestGetVMsGetAliasesFails(t *testing.T) {
	resources := initGetVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	vmIDs := []ids.ID{id1, id2}
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}

	resources.mockVMManager.ListFactoriesF = func() ([]ids.ID, error) {
		return vmIDs, nil
	}
	resources.mockVMManager.AliasesF = func(id ids.ID) ([]string, error) {
		switch id {
		case id1:
			return alias1, nil
		case id2:
			return nil, errTest
		default:
			return nil, errTest
		}
	}

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)
	require.ErrorIs(t, err, errTest)
}
