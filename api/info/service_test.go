// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var errOops = errors.New("oops")

type getVMsTest struct {
	info          *Info
	ctrl          *gomock.Controller
	mockLog       *logging.MockLogger
	mockVMManager *vms.MockManager
}

func initGetVMsTest(t *testing.T) *getVMsTest {
	ctrl := gomock.NewController(t)

	service := Info{}
	mockLog := logging.NewMockLogger(ctrl)
	mockVMManager := vms.NewMockManager(ctrl)

	service.log = mockLog
	service.VMManager = mockVMManager

	return &getVMsTest{
		info:          &service,
		ctrl:          ctrl,
		mockLog:       mockLog,
		mockVMManager: mockVMManager,
	}
}

// Tests GetVMs in the happy-case
func TestGetVMsSuccess(t *testing.T) {
	resources := initGetVMsTest(t)
	defer resources.ctrl.Finish()

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

	resources.mockLog.EXPECT().Debug(gomock.Any()).Times(1)
	resources.mockVMManager.EXPECT().ListFactories().Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(alias2, nil)

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)

	assert.Equal(t, expectedVMRegistry, reply.VMs)
	assert.Equal(t, err, nil)
}

// Tests GetVMs if we fail to list our vms.
func TestGetVMsVMsListFactoriesFails(t *testing.T) {
	resources := initGetVMsTest(t)
	defer resources.ctrl.Finish()

	resources.mockLog.EXPECT().Debug(gomock.Any()).Times(1)
	resources.mockVMManager.EXPECT().ListFactories().Times(1).Return(nil, errOops)

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)

	assert.Equal(t, errOops, err)
}

// Tests GetVMs if we can't get our vm aliases.
func TestGetVMsGetAliasesFails(t *testing.T) {
	resources := initGetVMsTest(t)
	defer resources.ctrl.Finish()

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	vmIDs := []ids.ID{id1, id2}
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}

	resources.mockLog.EXPECT().Debug(gomock.Any()).Times(1)
	resources.mockVMManager.EXPECT().ListFactories().Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(nil, errOops)

	reply := GetVMsReply{}
	err := resources.info.GetVMs(nil, nil, &reply)

	assert.Equal(t, err, errOops)
}
