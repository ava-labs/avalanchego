// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/registry"
)

var errOops = errors.New("oops")

type loadVMsTest struct {
	admin          *Admin
	ctrl           *gomock.Controller
	mockLog        *logging.MockLogger
	mockVMManager  *vms.MockManager
	mockVMRegistry *registry.MockVMRegistry
}

func initLoadVMsTest(t *testing.T) *loadVMsTest {
	ctrl := gomock.NewController(t)

	mockLog := logging.NewMockLogger(ctrl)
	mockVMRegistry := registry.NewMockVMRegistry(ctrl)
	mockVMManager := vms.NewMockManager(ctrl)

	return &loadVMsTest{
		admin: &Admin{Config: Config{
			Log:        mockLog,
			VMRegistry: mockVMRegistry,
			VMManager:  mockVMManager,
		}},
		ctrl:           ctrl,
		mockLog:        mockLog,
		mockVMManager:  mockVMManager,
		mockVMRegistry: mockVMRegistry,
	}
}

// Tests behavior for LoadVMs if everything succeeds.
func TestLoadVMsSuccess(t *testing.T) {
	resources := initLoadVMsTest(t)
	defer resources.ctrl.Finish()

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	newVMs := []ids.ID{id1, id2}
	failedVMs := map[ids.ID]error{
		ids.GenerateTestID(): errors.New("failed for some reason"),
	}
	// every vm is at least aliased to itself.
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}
	alias2 := []string{id2.String(), "vm2-alias-1", "vm2-alias-2"}
	// we expect that we dedup the redundant alias of vmId.
	expectedVMRegistry := map[ids.ID][]string{
		id1: alias1[1:],
		id2: alias2[1:],
	}

	resources.mockLog.EXPECT().Debug(gomock.Any()).Times(1)
	resources.mockVMRegistry.EXPECT().ReloadWithReadLock().Times(1).Return(newVMs, failedVMs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(alias2, nil)

	// execute test
	reply := LoadVMsReply{}
	err := resources.admin.LoadVMs(nil, nil, &reply)

	assert.Equal(t, expectedVMRegistry, reply.NewVMs)
	assert.Equal(t, err, nil)
}

// Tests behavior for LoadVMs if we fail to reload vms.
func TestLoadVMsReloadFails(t *testing.T) {
	resources := initLoadVMsTest(t)
	defer resources.ctrl.Finish()

	resources.mockLog.EXPECT().Debug(gomock.Any()).Times(1)
	// Reload fails
	resources.mockVMRegistry.EXPECT().ReloadWithReadLock().Times(1).Return(nil, nil, errOops)

	reply := LoadVMsReply{}
	err := resources.admin.LoadVMs(nil, nil, &reply)

	assert.Equal(t, err, errOops)
}

// Tests behavior for LoadVMs if we fail to fetch our aliases
func TestLoadVMsGetAliasesFails(t *testing.T) {
	resources := initLoadVMsTest(t)
	defer resources.ctrl.Finish()

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	newVMs := []ids.ID{id1, id2}
	failedVMs := map[ids.ID]error{
		ids.GenerateTestID(): errors.New("failed for some reason"),
	}
	// every vm is at least aliased to itself.
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}

	resources.mockLog.EXPECT().Debug(gomock.Any()).Times(1)
	resources.mockVMRegistry.EXPECT().ReloadWithReadLock().Times(1).Return(newVMs, failedVMs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(nil, errOops)

	reply := LoadVMsReply{}
	err := resources.admin.LoadVMs(nil, nil, &reply)

	assert.Equal(t, err, errOops)
}
