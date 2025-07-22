// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/vmsmock"
)

var errTest = errors.New("non-nil error")

type getVMsTest struct {
	info          *Info
	mockVMManager *vmsmock.Manager
}

func initGetVMsTest(t *testing.T) *getVMsTest {
	ctrl := gomock.NewController(t)
	mockVMManager := vmsmock.NewManager(ctrl)
	return &getVMsTest{
		info: &Info{
			Parameters: Parameters{},
			VMManager:  mockVMManager,
			Log:        logging.NoLog{},
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

	resources.mockVMManager.EXPECT().ListFactories().Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(alias2, nil)

	reply := GetVMsReply{}
	require.NoError(resources.info.GetVMs(nil, nil, &reply))
	require.Equal(expectedVMRegistry, reply.VMs)
}

// Tests GetVMs if we fail to list our vms.
func TestGetVMsVMsListFactoriesFails(t *testing.T) {
	resources := initGetVMsTest(t)

	resources.mockVMManager.EXPECT().ListFactories().Times(1).Return(nil, errTest)

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

	resources.mockVMManager.EXPECT().ListFactories().Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(nil, errTest)

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)
	require.ErrorIs(t, err, errTest)
}
