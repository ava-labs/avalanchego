// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
)

func TestChangeNotifierStateSyncableVM(t *testing.T) {
	ctrl := gomock.NewController(t)
	fullVM := blockmock.NewFullVM(ctrl)

	fullVM.EXPECT().StateSyncEnabled(gomock.Any()).Return(true, nil)
	fullVM.EXPECT().GetOngoingSyncStateSummary(gomock.Any()).Return(&blocktest.StateSummary{}, nil)
	fullVM.EXPECT().GetLastStateSummary(gomock.Any()).Return(&blocktest.StateSummary{}, nil)
	fullVM.EXPECT().ParseStateSummary(gomock.Any(), gomock.Any()).Return(&blocktest.StateSummary{}, nil)
	fullVM.EXPECT().GetStateSummary(gomock.Any(), gomock.Any()).Return(&blocktest.StateSummary{}, nil)

	vm := &blockmock.ChainVM{}

	for _, testCase := range []struct {
		name string
		f    func(*testing.T, *block.ChangeNotifier)
		vm   block.ChainVM
	}{
		{
			name: "StateSyncEnabled",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.StateSyncEnabled(t.Context())
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "GetOngoingSyncStateSummary",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetOngoingSyncStateSummary(t.Context())
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "GetLastStateSummary",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetLastStateSummary(t.Context())
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "ParseStateSummary",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.ParseStateSummary(t.Context(), []byte{})
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "GetStateSummary",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetStateSummary(t.Context(), 0)
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "StateSyncEnabled-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				ok, err := n.StateSyncEnabled(t.Context())
				require.NoError(t, err)
				require.False(t, ok, "expected StateSyncEnabled to return false")
			},
			vm: vm,
		},
		{
			name: "GetOngoingSyncStateSummary-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetOngoingSyncStateSummary(t.Context())
				require.ErrorIs(t, err, block.ErrStateSyncableVMNotImplemented)
			},
			vm: vm,
		},
		{
			name: "GetLastStateSummary-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetLastStateSummary(t.Context())
				require.ErrorIs(t, err, block.ErrStateSyncableVMNotImplemented)
			},
		},
		{
			name: "ParseStateSummary-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.ParseStateSummary(t.Context(), []byte{})
				require.ErrorIs(t, err, block.ErrStateSyncableVMNotImplemented)
			},
		},
		{
			name: "GetStateSummary-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetStateSummary(t.Context(), 0)
				require.ErrorIs(t, err, block.ErrStateSyncableVMNotImplemented)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nf := block.ChangeNotifier{
				ChainVM: testCase.vm,
			}
			testCase.f(t, &nf)
		})
	}
}

func TestChangeNotifierBatchedChainVM(t *testing.T) {
	ctrl := gomock.NewController(t)
	fullVM := blockmock.NewFullVM(ctrl)
	fullVM.EXPECT().GetAncestors(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([][]byte{}, nil)
	fullVM.EXPECT().BatchedParseBlock(gomock.Any(), gomock.Any()).Return([]snowman.Block{}, nil)

	vm := &blockmock.ChainVM{}

	for _, testCase := range []struct {
		name string
		f    func(*testing.T, *block.ChangeNotifier)
		vm   block.ChainVM
	}{
		{
			name: "BatchedParseBlock",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.BatchedParseBlock(t.Context(), [][]byte{})
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "GetAncestors",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetAncestors(t.Context(), ids.Empty, 0, 0, 0)
				require.NoError(t, err)
			},
			vm: fullVM,
		},
		{
			name: "BatchedParseBlock-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.BatchedParseBlock(t.Context(), [][]byte{})
				require.ErrorIs(t, err, block.ErrRemoteVMNotImplemented)
			},
			vm: vm,
		},
		{
			name: "GetAncestors-not-implemented",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.GetAncestors(t.Context(), ids.Empty, 0, 0, 0)
				require.ErrorIs(t, err, block.ErrRemoteVMNotImplemented)
			},
			vm: vm,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nf := block.ChangeNotifier{
				ChainVM: testCase.vm,
			}
			testCase.f(t, &nf)
		})
	}
}

func TestChangeNotifierNormal(t *testing.T) {
	ctrl := gomock.NewController(t)

	tvm := blockmock.NewFullVM(ctrl)
	tvm.EXPECT().BuildBlock(gomock.Any()).Return(&snowmantest.Block{}, nil)
	tvm.EXPECT().SetState(gomock.Any(), gomock.Any()).Return(nil)
	tvm.EXPECT().SetPreference(gomock.Any(), gomock.Any()).Return(nil)

	for _, testCase := range []struct {
		name string
		f    func(*testing.T, *block.ChangeNotifier)
	}{
		{
			name: "SetPreference",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				require.NoError(t, n.SetPreference(t.Context(), ids.Empty))
			},
		},
		{
			name: "SetState",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				require.NoError(t, n.SetState(t.Context(), snow.NormalOp))
			},
		},
		{
			name: "BuildBlock",
			f: func(t *testing.T, n *block.ChangeNotifier) {
				_, err := n.BuildBlock(t.Context())
				require.NoError(t, err)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var invoked bool
			nf := block.ChangeNotifier{
				OnChange: func() {
					invoked = true
				},
				ChainVM: tvm,
			}
			testCase.f(t, &nf)
			require.True(t, invoked, "expected to have been invoked")
		})
	}
}

func TestChangeNotifierSetPreference(t *testing.T) {
	ctrl := gomock.NewController(t)

	tvm := blockmock.NewFullVM(ctrl)
	tvm.EXPECT().SetPreference(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	var invoked bool
	nf := block.ChangeNotifier{
		OnChange: func() {
			invoked = true
		},
		ChainVM: tvm,
	}

	// First time SetPreference is called, it should invoke OnChange
	require.NoError(t, nf.SetPreference(t.Context(), ids.Empty), "expected SetPreference to succeed")
	require.True(t, invoked, "expected to have been invoked on first SetPreference call")

	invoked = false
	// Second time SetPreference is called with the same block ID, it should not invoke OnChange
	require.NoError(t, nf.SetPreference(t.Context(), ids.Empty), "expected SetPreference to succeed on second call with same block ID")
	require.False(t, invoked, "expected not to have been invoked on second SetPreference call with same block ID")

	invoked = false
	// Third time SetPreference is called with a different block ID, it should invoke OnChange again
	testID := ids.GenerateTestID()
	require.NoError(t, nf.SetPreference(t.Context(), testID), "expected SetPreference to succeed on third call with different block ID")
	require.True(t, invoked, "expected to have been invoked on third SetPreference call with different block ID")

	invoked = false
	// Fourth time SetPreference is called with the same block ID, it should not invoke OnChange
	require.NoError(t, nf.SetPreference(t.Context(), testID), "expected SetPreference to succeed on fourth call with same block ID")
	require.False(t, invoked, "expected not to have been invoked on fourth SetPreference call with same block ID")
}
