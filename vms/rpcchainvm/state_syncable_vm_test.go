// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/hashicorp/go-plugin"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
)

var (
	_ block.ChainVM         = StateSyncEnabledMock{}
	_ block.StateSyncableVM = StateSyncEnabledMock{}

	// a summary to be returned in some UTs
	mockedSummary = &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: 2022,
		BytesV:  []byte("summary"),
	}

	// a fictitious error unrelated to state sync
	errBrokenConnectionOrSomething = fmt.Errorf("brokenConnectionOrSomething")
	errNothingToParse              = fmt.Errorf("nil summary bytes. Nothing to parse")
)

type StateSyncEnabledMock struct {
	*mocks.ChainVM
	*mocks.MockStateSyncableVM
}

func stateSyncEnabledTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "stateSyncEnabledTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:             &mocks.ChainVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.EXPECT().StateSyncEnabled().Return(false, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.EXPECT().StateSyncEnabled().Return(false, nil).Times(1),
			ssVM.EXPECT().StateSyncEnabled().Return(true, nil).Times(1),
			ssVM.EXPECT().StateSyncEnabled().Return(false, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func getOngoingSyncStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "getOngoingSyncStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:             &mocks.ChainVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.EXPECT().GetOngoingSyncStateSummary().Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.EXPECT().GetOngoingSyncStateSummary().Return(mockedSummary, nil).Times(1),
			ssVM.EXPECT().GetOngoingSyncStateSummary().Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func getLastStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "getLastStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:             &mocks.ChainVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.EXPECT().GetLastStateSummary().Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.EXPECT().GetLastStateSummary().Return(mockedSummary, nil).Times(1),
			ssVM.EXPECT().GetLastStateSummary().Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func parseStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "parseStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:             &mocks.ChainVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).Return(nil, errNothingToParse).Times(1),
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func getStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "getStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:             &mocks.ChainVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.EXPECT().GetStateSummary(gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.EXPECT().GetStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.EXPECT().GetStateSummary(gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func acceptStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "acceptStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:             &mocks.ChainVM{},
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.EXPECT().GetStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).DoAndReturn(
				func(summaryBytes []byte) (block.StateSummary, error) {
					// setup summary to be accepted before returning it
					mockedSummary.AcceptF = func() (bool, error) { return true, nil }
					return mockedSummary, nil
				},
			).Times(1),
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).DoAndReturn(
				func(summaryBytes []byte) (block.StateSummary, error) {
					// setup summary to be skipped before returning it
					mockedSummary.AcceptF = func() (bool, error) { return false, nil }
					return mockedSummary, nil
				},
			).Times(1),
			ssVM.EXPECT().ParseStateSummary(gomock.Any()).DoAndReturn(
				func(summaryBytes []byte) (block.StateSummary, error) {
					// setup summary to fail accept
					mockedSummary.AcceptF = func() (bool, error) { return false, errBrokenConnectionOrSomething }
					return mockedSummary, nil
				},
			).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func buildClientHelper(assert *assert.Assertions, testKey string, mockedPlugin plugin.Plugin) (*VMClient, *plugin.Client) {
	process := helperProcess(testKey)
	c := plugin.NewClient(&plugin.ClientConfig{
		Cmd:              process,
		HandshakeConfig:  TestHandshake,
		Plugins:          plugin.PluginSet{testKey: mockedPlugin},
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})

	_, err := c.Start()
	assert.NoErrorf(err, "failed to start plugin: %v", err)
	assert.True(c.Protocol() == plugin.ProtocolGRPC)

	// Get the plugin client.
	client, err := c.Client()
	assert.NoErrorf(err, "failed to get plugin client: %v", err)

	// Grab the vm implementation.
	raw, err := client.Dispense(testKey)
	assert.NoErrorf(err, "failed to dispense plugin: %v", err)

	// Get vm client.
	vm, ok := raw.(*VMClient)
	assert.True(ok)

	return vm, c
}

func TestStateSyncEnabled(t *testing.T) {
	assert := assert.New(t)
	testKey := stateSyncEnabledTestKey

	mockedPlugin, ctrl := stateSyncEnabledTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(assert, testKey, mockedPlugin)
	defer c.Kill()

	// test state sync not implemented
	// Note that enabled == false is returned rather than
	// common.ErrStateSyncableVMNotImplemented
	enabled, err := vm.StateSyncEnabled()
	assert.NoError(err)
	assert.False(enabled)

	// test state sync disabled
	enabled, err = vm.StateSyncEnabled()
	assert.NoError(err)
	assert.False(enabled)

	// test state sync enabled
	enabled, err = vm.StateSyncEnabled()
	assert.NoError(err)
	assert.True(enabled)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.StateSyncEnabled()
	assert.Error(err)
}

func TestGetOngoingSyncStateSummary(t *testing.T) {
	assert := assert.New(t)
	testKey := getOngoingSyncStateSummaryTestKey

	mockedPlugin, ctrl := getOngoingSyncStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(assert, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.GetOngoingSyncStateSummary()
	assert.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetOngoingSyncStateSummary()
	assert.NoError(err)
	assert.Equal(mockedSummary.ID(), summary.ID())
	assert.Equal(mockedSummary.Height(), summary.Height())
	assert.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.GetOngoingSyncStateSummary()
	assert.Error(err)
}

func TestGetLastStateSummary(t *testing.T) {
	assert := assert.New(t)
	testKey := getLastStateSummaryTestKey

	mockedPlugin, ctrl := getLastStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(assert, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.GetLastStateSummary()
	assert.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetLastStateSummary()
	assert.NoError(err)
	assert.Equal(mockedSummary.ID(), summary.ID())
	assert.Equal(mockedSummary.Height(), summary.Height())
	assert.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.GetLastStateSummary()
	assert.Error(err)
}

func TestParseStateSummary(t *testing.T) {
	assert := assert.New(t)
	testKey := parseStateSummaryTestKey

	mockedPlugin, ctrl := parseStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(assert, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.ParseStateSummary(mockedSummary.Bytes())
	assert.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful parsing
	summary, err := vm.ParseStateSummary(mockedSummary.Bytes())
	assert.NoError(err)
	assert.Equal(mockedSummary.ID(), summary.ID())
	assert.Equal(mockedSummary.Height(), summary.Height())
	assert.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test parsing nil summary
	_, err = vm.ParseStateSummary(nil)
	assert.Error(err)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.ParseStateSummary(mockedSummary.Bytes())
	assert.Error(err)
}

func TestGetStateSummary(t *testing.T) {
	assert := assert.New(t)
	testKey := getStateSummaryTestKey

	mockedPlugin, ctrl := getStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(assert, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.GetStateSummary(mockedSummary.Height())
	assert.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetStateSummary(mockedSummary.Height())
	assert.NoError(err)
	assert.Equal(mockedSummary.ID(), summary.ID())
	assert.Equal(mockedSummary.Height(), summary.Height())
	assert.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.GetStateSummary(mockedSummary.Height())
	assert.Error(err)
}

func TestAcceptStateSummary(t *testing.T) {
	assert := assert.New(t)
	testKey := acceptStateSummaryTestKey

	mockedPlugin, ctrl := acceptStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(assert, testKey, mockedPlugin)
	defer c.Kill()

	// retrieve the summary first
	summary, err := vm.GetStateSummary(mockedSummary.Height())
	assert.NoError(err)

	// test accepted Summary
	accepted, err := summary.Accept()
	assert.NoError(err)
	assert.True(accepted)

	// test skipped Summary
	accepted, err = summary.Accept()
	assert.NoError(err)
	assert.False(accepted)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = summary.Accept()
	assert.Error(err)
}
