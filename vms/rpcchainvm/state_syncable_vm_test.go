// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/hashicorp/go-plugin"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ block.ChainVM         = StateSyncEnabledMock{}
	_ block.StateSyncableVM = StateSyncEnabledMock{}

	preSummaryHeight = uint64(1789)
	SummaryHeight    = uint64(2022)

	// a summary to be returned in some UTs
	mockedSummary = &block.TestStateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: SummaryHeight,
		BytesV:  []byte("summary"),
	}

	// last accepted blocks data before and after summary is accepted
	preSummaryBlk = &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{'f', 'i', 'r', 's', 't', 'B', 'l', 'K'},
			StatusV: choices.Accepted,
		},
		HeightV: preSummaryHeight,
		ParentV: ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'B', 'l', 'k'},
	}

	summaryBlk = &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'B', 'l', 'K'},
			StatusV: choices.Accepted,
		},
		HeightV: SummaryHeight,
		ParentV: ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'B', 'l', 'k'},
	}

	// a fictitious error unrelated to state sync
	errBrokenConnectionOrSomething = errors.New("brokenConnectionOrSomething")
	errNothingToParse              = errors.New("nil summary bytes. Nothing to parse")
)

type StateSyncEnabledMock struct {
	*mocks.MockChainVM
	*mocks.MockStateSyncableVM
}

func stateSyncEnabledTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "stateSyncEnabledTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockStateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(false, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(false, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(true, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(false, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func getOngoingSyncStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "getOngoingSyncStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockStateSyncableVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func getLastStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "getLastStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockStateSyncableVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func parseStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "parseStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(nil, errNothingToParse).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func getStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "getStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockStateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func acceptStateSummaryTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "acceptStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockStateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to be accepted before returning it
					mockedSummary.AcceptF = func(context.Context) (bool, error) {
						return true, nil
					}
					return mockedSummary, nil
				},
			).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to be skipped before returning it
					mockedSummary.AcceptF = func(context.Context) (bool, error) {
						return false, nil
					}
					return mockedSummary, nil
				},
			).Times(1),
			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to fail accept
					mockedSummary.AcceptF = func(context.Context) (bool, error) {
						return false, errBrokenConnectionOrSomething
					}
					return mockedSummary, nil
				},
			).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func lastAcceptedBlockPostStateSummaryAcceptTestPlugin(t *testing.T, loadExpectations bool) (plugin.Plugin, *gomock.Controller) {
	// test key is "lastAcceptedBlockPostStateSummaryAcceptTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		MockChainVM:         mocks.NewMockChainVM(ctrl),
		MockStateSyncableVM: mocks.NewMockStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.MockChainVM.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(),
			).Return(nil).Times(1),
			ssVM.MockChainVM.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			ssVM.MockChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			ssVM.MockStateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to be accepted before returning it
					mockedSummary.AcceptF = func(context.Context) (bool, error) {
						return true, nil
					}
					return mockedSummary, nil
				},
			).Times(2),

			ssVM.MockChainVM.EXPECT().SetState(gomock.Any(), gomock.Any()).Return(nil).Times(1),
			ssVM.MockChainVM.EXPECT().LastAccepted(gomock.Any()).Return(summaryBlk.ID(), nil).Times(1),
			ssVM.MockChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(summaryBlk, nil).Times(1),
		)
	}

	return New(ssVM), ctrl
}

func buildClientHelper(require *require.Assertions, testKey string, mockedPlugin plugin.Plugin) (*VMClient, *plugin.Client) {
	process := helperProcess(testKey)
	c := plugin.NewClient(&plugin.ClientConfig{
		Cmd:              process,
		HandshakeConfig:  TestHandshake,
		Plugins:          plugin.PluginSet{testKey: mockedPlugin},
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})

	_, err := c.Start()
	require.NoErrorf(err, "failed to start plugin: %v", err)
	require.True(c.Protocol() == plugin.ProtocolGRPC)

	// Get the plugin client.
	client, err := c.Client()
	require.NoErrorf(err, "failed to get plugin client: %v", err)

	// Grab the vm implementation.
	raw, err := client.Dispense(testKey)
	require.NoErrorf(err, "failed to dispense plugin: %v", err)

	// Get vm client.
	vm, ok := raw.(*VMClient)
	require.True(ok)

	return vm, c
}

func TestStateSyncEnabled(t *testing.T) {
	require := require.New(t)
	testKey := stateSyncEnabledTestKey

	mockedPlugin, ctrl := stateSyncEnabledTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// test state sync not implemented
	// Note that enabled == false is returned rather than
	// common.ErrStateSyncableVMNotImplemented
	enabled, err := vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// test state sync disabled
	enabled, err = vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// test state sync enabled
	enabled, err = vm.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.StateSyncEnabled(context.Background())
	require.Error(err)
}

func TestGetOngoingSyncStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getOngoingSyncStateSummaryTestKey

	mockedPlugin, ctrl := getOngoingSyncStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.GetOngoingSyncStateSummary(context.Background())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.GetOngoingSyncStateSummary(context.Background())
	require.Error(err)
}

func TestGetLastStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getLastStateSummaryTestKey

	mockedPlugin, ctrl := getLastStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.GetLastStateSummary(context.Background())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.GetLastStateSummary(context.Background())
	require.Error(err)
}

func TestParseStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := parseStateSummaryTestKey

	mockedPlugin, ctrl := parseStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful parsing
	summary, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test parsing nil summary
	_, err = vm.ParseStateSummary(context.Background(), nil)
	require.Error(err)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.Error(err)
}

func TestGetStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getStateSummaryTestKey

	mockedPlugin, ctrl := getStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// test unimplemented case; this is just a guard
	_, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.Equal(block.ErrStateSyncableVMNotImplemented, err)

	// test successful retrieval
	summary, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.NoError(err)
	require.Equal(mockedSummary.ID(), summary.ID())
	require.Equal(mockedSummary.Height(), summary.Height())
	require.Equal(mockedSummary.Bytes(), summary.Bytes())

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.Error(err)
}

func TestAcceptStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := acceptStateSummaryTestKey

	mockedPlugin, ctrl := acceptStateSummaryTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// retrieve the summary first
	summary, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.NoError(err)

	// test accepted Summary
	accepted, err := summary.Accept(context.Background())
	require.NoError(err)
	require.True(accepted)

	// test skipped Summary
	accepted, err = summary.Accept(context.Background())
	require.NoError(err)
	require.False(accepted)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = summary.Accept(context.Background())
	require.Error(err)
}

// Show that LastAccepted call returns the right answer after a StateSummary
// is accepted AND engine state moves to bootstrapping
func TestLastAcceptedBlockPostStateSummaryAccept(t *testing.T) {
	require := require.New(t)
	testKey := lastAcceptedBlockPostStateSummaryAcceptTestKey

	mockedPlugin, ctrl := lastAcceptedBlockPostStateSummaryAcceptTestPlugin(t, false /*loadExpectations*/)
	defer ctrl.Finish()

	// Create and start the plugin
	vm, c := buildClientHelper(require, testKey, mockedPlugin)
	defer c.Kill()

	// Step 1: initialize VM and check initial LastAcceptedBlock
	ctx := snow.DefaultContextTest()
	dbManager := manager.NewMemDB(version.Semantic1_0_0)
	dbManager = dbManager.NewPrefixDBManager([]byte{})

	require.NoError(vm.Initialize(context.Background(), ctx, dbManager, nil, nil, nil, nil, nil, nil))

	blkID, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(preSummaryBlk.ID(), blkID)

	lastBlk, err := vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(preSummaryBlk.Height(), lastBlk.Height())

	// Step 2: pick a state summary to an higher height and accept it
	summary, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.NoError(err)

	accepted, err := summary.Accept(context.Background())
	require.NoError(err)
	require.True(accepted)

	// State Sync accept does not duly update LastAccepted block information
	// since state sync can complete asynchronously
	blkID, err = vm.LastAccepted(context.Background())
	require.NoError(err)

	lastBlk, err = vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(preSummaryBlk.Height(), lastBlk.Height())

	// Setting state to bootstrapping duly update last accepted block
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))

	blkID, err = vm.LastAccepted(context.Background())
	require.NoError(err)

	lastBlk, err = vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(summary.Height(), lastBlk.Height())
}
