// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime/subprocess"
)

var (
	_ block.ChainVM         = StateSyncEnabledMock{}
	_ block.StateSyncableVM = StateSyncEnabledMock{}

	preSummaryHeight = uint64(1789)
	SummaryHeight    = uint64(2022)

	// a summary to be returned in some UTs
	mockedSummary = &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: SummaryHeight,
		BytesV:  []byte("summary"),
	}

	// last accepted blocks data before and after summary is accepted
	preSummaryBlk = &snowmantest.Block{
		Decidable: snowtest.Decidable{
			IDV:    ids.ID{'f', 'i', 'r', 's', 't', 'B', 'l', 'K'},
			Status: snowtest.Accepted,
		},
		HeightV: preSummaryHeight,
		ParentV: ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'B', 'l', 'k'},
	}

	summaryBlk = &snowmantest.Block{
		Decidable: snowtest.Decidable{
			IDV:    ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'B', 'l', 'K'},
			Status: snowtest.Accepted,
		},
		HeightV: SummaryHeight,
		ParentV: ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'B', 'l', 'k'},
	}

	// a fictitious error unrelated to state sync
	errBrokenConnectionOrSomething = errors.New("brokenConnectionOrSomething")
	errNothingToParse              = errors.New("nil summary bytes. Nothing to parse")
)

type StateSyncEnabledMock struct {
	*blockmock.ChainVM
	*blockmock.StateSyncableVM
}

func stateSyncEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "stateSyncEnabledTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.StateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(false, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.StateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(false, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(true, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(false, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return ssVM
}

func getOngoingSyncStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "getOngoingSyncStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.StateSyncableVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.StateSyncableVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return ssVM
}

func getLastStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "getLastStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.StateSyncableVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.StateSyncableVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return ssVM
}

func parseStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "parseStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(nil, errNothingToParse).Times(1),
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return ssVM
}

func getStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "getStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.StateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(nil, block.ErrStateSyncableVMNotImplemented).Times(1),
			ssVM.StateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(nil, errBrokenConnectionOrSomething).Times(1),
		)
	}

	return ssVM
}

func acceptStateSummaryTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "acceptStateSummaryTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.StateSyncableVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(mockedSummary, nil).Times(1),
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to be accepted before returning it
					mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
						return block.StateSyncStatic, nil
					}
					return mockedSummary, nil
				},
			).Times(1),
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to be skipped before returning it
					mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
						return block.StateSyncSkipped, nil
					}
					return mockedSummary, nil
				},
			).Times(1),
			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to fail accept
					mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
						return block.StateSyncSkipped, errBrokenConnectionOrSomething
					}
					return mockedSummary, nil
				},
			).Times(1),
		)
	}

	return ssVM
}

func lastAcceptedBlockPostStateSummaryAcceptTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "lastAcceptedBlockPostStateSummaryAcceptTestKey"

	// create mock
	ctrl := gomock.NewController(t)
	ssVM := StateSyncEnabledMock{
		ChainVM:         blockmock.NewChainVM(ctrl),
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	if loadExpectations {
		gomock.InOrder(
			ssVM.ChainVM.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(),
			).Return(nil).Times(1),
			ssVM.ChainVM.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			ssVM.ChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			ssVM.StateSyncableVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, []byte) (block.StateSummary, error) {
					// setup summary to be accepted before returning it
					mockedSummary.AcceptF = func(context.Context) (block.StateSyncMode, error) {
						return block.StateSyncStatic, nil
					}
					return mockedSummary, nil
				},
			).Times(2),

			ssVM.ChainVM.EXPECT().SetState(gomock.Any(), gomock.Any()).Return(nil).Times(1),
			ssVM.ChainVM.EXPECT().LastAccepted(gomock.Any()).Return(summaryBlk.ID(), nil).Times(1),
			ssVM.ChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(summaryBlk, nil).Times(1),
		)
	}

	return ssVM
}

func buildClientHelper(
	ctx context.Context,
	require *require.Assertions,
	testKey string,
) *VMClient {
	process := helperProcess(ctx, testKey)

	log := logging.NewLogger(
		testKey,
		logging.NewWrappedCore(
			logging.Info,
			originalStderr,
			logging.Colors.ConsoleEncoder(),
		),
	)

	listener, err := grpcutils.NewListener()
	require.NoError(err)

	status, stopper, err := subprocess.Bootstrap(
		context.Background(),
		listener,
		process,
		&subprocess.Config{
			Stderr:           log,
			Stdout:           io.Discard,
			Log:              log,
			HandshakeTimeout: runtime.DefaultHandshakeTimeout,
		},
	)
	require.NoError(err)

	clientConn, err := grpcutils.Dial(status.Addr)
	require.NoError(err)

	return NewClient(clientConn, stopper, status.Pid, nil, metrics.NewPrefixGatherer(), &logging.NoLog{})
}

func TestStateSyncEnabled(t *testing.T) {
	require := require.New(t)
	testKey := stateSyncEnabledTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

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
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestGetOngoingSyncStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getOngoingSyncStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

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
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestGetLastStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getLastStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

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
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestParseStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := parseStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

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
	require.Error(err) //nolint:forbidigo // currently returns grpc errors

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestGetStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := getStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

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
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

func TestAcceptStateSummary(t *testing.T) {
	require := require.New(t)
	testKey := acceptStateSummaryTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

	// retrieve the summary first
	summary, err := vm.GetStateSummary(context.Background(), mockedSummary.Height())
	require.NoError(err)

	// test status Summary
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)

	// test skipped Summary
	status, err = summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncSkipped, status)

	// test a non-special error.
	// TODO: retrieve exact error
	_, err = summary.Accept(context.Background())
	require.Error(err) //nolint:forbidigo // currently returns grpc errors
}

// Show that LastAccepted call returns the right answer after a StateSummary
// is accepted AND engine state moves to bootstrapping
func TestLastAcceptedBlockPostStateSummaryAccept(t *testing.T) {
	require := require.New(t)
	testKey := lastAcceptedBlockPostStateSummaryAcceptTestKey

	// Create and start the plugin
	vm := buildClientHelper(context.Background(), require, testKey)
	defer vm.runtime.Stop(context.Background())

	// Step 1: initialize VM and check initial LastAcceptedBlock
	ctx := snowtest.Context(t, snowtest.CChainID)

	require.NoError(vm.Initialize(context.Background(), ctx, prefixdb.New([]byte{}, memdb.New()), nil, nil, nil, nil, nil))

	blkID, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(preSummaryBlk.ID(), blkID)

	lastBlk, err := vm.GetBlock(context.Background(), blkID)
	require.NoError(err)
	require.Equal(preSummaryBlk.Height(), lastBlk.Height())

	// Step 2: pick a state summary to an higher height and accept it
	summary, err := vm.ParseStateSummary(context.Background(), mockedSummary.Bytes())
	require.NoError(err)

	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(block.StateSyncStatic, status)

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
