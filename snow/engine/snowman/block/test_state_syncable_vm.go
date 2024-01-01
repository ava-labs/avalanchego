// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	_ StateSyncableVM = (*TestStateSyncableVM)(nil)

	errStateSyncEnabled           = errors.New("unexpectedly called StateSyncEnabled")
	errStateSyncGetOngoingSummary = errors.New("unexpectedly called StateSyncGetOngoingSummary")
	errGetLastStateSummary        = errors.New("unexpectedly called GetLastStateSummary")
	errParseStateSummary          = errors.New("unexpectedly called ParseStateSummary")
	errGetStateSummary            = errors.New("unexpectedly called GetStateSummary")
)

type TestStateSyncableVM struct {
	T *testing.T

	CantStateSyncEnabled,
	CantStateSyncGetOngoingSummary,
	CantGetLastStateSummary,
	CantParseStateSummary,
	CantGetStateSummary bool

	StateSyncEnabledF           func(context.Context) (bool, error)
	GetOngoingSyncStateSummaryF func(context.Context) (StateSummary, error)
	GetLastStateSummaryF        func(context.Context) (StateSummary, error)
	ParseStateSummaryF          func(ctx context.Context, summaryBytes []byte) (StateSummary, error)
	GetStateSummaryF            func(ctx context.Context, summaryHeight uint64) (StateSummary, error)
}

func (vm *TestStateSyncableVM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.StateSyncEnabledF != nil {
		return vm.StateSyncEnabledF(ctx)
	}
	if vm.CantStateSyncEnabled && vm.T != nil {
		require.FailNow(vm.T, errStateSyncEnabled.Error())
	}
	return false, errStateSyncEnabled
}

func (vm *TestStateSyncableVM) GetOngoingSyncStateSummary(ctx context.Context) (StateSummary, error) {
	if vm.GetOngoingSyncStateSummaryF != nil {
		return vm.GetOngoingSyncStateSummaryF(ctx)
	}
	if vm.CantStateSyncGetOngoingSummary && vm.T != nil {
		require.FailNow(vm.T, errStateSyncGetOngoingSummary.Error())
	}
	return nil, errStateSyncGetOngoingSummary
}

func (vm *TestStateSyncableVM) GetLastStateSummary(ctx context.Context) (StateSummary, error) {
	if vm.GetLastStateSummaryF != nil {
		return vm.GetLastStateSummaryF(ctx)
	}
	if vm.CantGetLastStateSummary && vm.T != nil {
		require.FailNow(vm.T, errGetLastStateSummary.Error())
	}
	return nil, errGetLastStateSummary
}

func (vm *TestStateSyncableVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (StateSummary, error) {
	if vm.ParseStateSummaryF != nil {
		return vm.ParseStateSummaryF(ctx, summaryBytes)
	}
	if vm.CantParseStateSummary && vm.T != nil {
		require.FailNow(vm.T, errParseStateSummary.Error())
	}
	return nil, errParseStateSummary
}

func (vm *TestStateSyncableVM) GetStateSummary(ctx context.Context, summaryHeight uint64) (StateSummary, error) {
	if vm.GetStateSummaryF != nil {
		return vm.GetStateSummaryF(ctx, summaryHeight)
	}
	if vm.CantGetStateSummary && vm.T != nil {
		require.FailNow(vm.T, errGetStateSummary.Error())
	}
	return nil, errGetStateSummary
}
