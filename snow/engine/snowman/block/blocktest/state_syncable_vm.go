// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ block.StateSyncableVM = (*StateSyncableVM)(nil)

	errStateSyncEnabled           = errors.New("unexpectedly called StateSyncEnabled")
	errStateSyncGetOngoingSummary = errors.New("unexpectedly called StateSyncGetOngoingSummary")
	errGetLastStateSummary        = errors.New("unexpectedly called GetLastStateSummary")
	errParseStateSummary          = errors.New("unexpectedly called ParseStateSummary")
	errGetStateSummary            = errors.New("unexpectedly called GetStateSummary")
)

type StateSyncableVM struct {
	T *testing.T

	CantStateSyncEnabled,
	CantStateSyncGetOngoingSummary,
	CantGetLastStateSummary,
	CantParseStateSummary,
	CantGetStateSummary bool

	StateSyncEnabledF           func(context.Context) (bool, error)
	GetOngoingSyncStateSummaryF func(context.Context) (block.StateSummary, error)
	GetLastStateSummaryF        func(context.Context) (block.StateSummary, error)
	ParseStateSummaryF          func(ctx context.Context, summaryBytes []byte) (block.StateSummary, error)
	GetStateSummaryF            func(ctx context.Context, summaryHeight uint64) (block.StateSummary, error)
}

func (vm *StateSyncableVM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.StateSyncEnabledF != nil {
		return vm.StateSyncEnabledF(ctx)
	}
	if vm.CantStateSyncEnabled && vm.T != nil {
		require.FailNow(vm.T, errStateSyncEnabled.Error())
	}
	return false, errStateSyncEnabled
}

func (vm *StateSyncableVM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.GetOngoingSyncStateSummaryF != nil {
		return vm.GetOngoingSyncStateSummaryF(ctx)
	}
	if vm.CantStateSyncGetOngoingSummary && vm.T != nil {
		require.FailNow(vm.T, errStateSyncGetOngoingSummary.Error())
	}
	return nil, errStateSyncGetOngoingSummary
}

func (vm *StateSyncableVM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.GetLastStateSummaryF != nil {
		return vm.GetLastStateSummaryF(ctx)
	}
	if vm.CantGetLastStateSummary && vm.T != nil {
		require.FailNow(vm.T, errGetLastStateSummary.Error())
	}
	return nil, errGetLastStateSummary
}

func (vm *StateSyncableVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if vm.ParseStateSummaryF != nil {
		return vm.ParseStateSummaryF(ctx, summaryBytes)
	}
	if vm.CantParseStateSummary && vm.T != nil {
		require.FailNow(vm.T, errParseStateSummary.Error())
	}
	return nil, errParseStateSummary
}

func (vm *StateSyncableVM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	if vm.GetStateSummaryF != nil {
		return vm.GetStateSummaryF(ctx, summaryHeight)
	}
	if vm.CantGetStateSummary && vm.T != nil {
		require.FailNow(vm.T, errGetStateSummary.Error())
	}
	return nil, errGetStateSummary
}
