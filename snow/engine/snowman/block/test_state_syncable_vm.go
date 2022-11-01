// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"
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

	StateSyncEnabledF           func() (bool, error)
	GetOngoingSyncStateSummaryF func() (StateSummary, error)
	GetLastStateSummaryF        func() (StateSummary, error)
	ParseStateSummaryF          func(summaryBytes []byte) (StateSummary, error)
	GetStateSummaryF            func(uint64) (StateSummary, error)
}

func (vm *TestStateSyncableVM) StateSyncEnabled() (bool, error) {
	if vm.StateSyncEnabledF != nil {
		return vm.StateSyncEnabledF()
	}
	if vm.CantStateSyncEnabled && vm.T != nil {
		vm.T.Fatal(errStateSyncEnabled)
	}
	return false, errStateSyncEnabled
}

func (vm *TestStateSyncableVM) GetOngoingSyncStateSummary() (StateSummary, error) {
	if vm.GetOngoingSyncStateSummaryF != nil {
		return vm.GetOngoingSyncStateSummaryF()
	}
	if vm.CantStateSyncGetOngoingSummary && vm.T != nil {
		vm.T.Fatal(errStateSyncGetOngoingSummary)
	}
	return nil, errStateSyncGetOngoingSummary
}

func (vm *TestStateSyncableVM) GetLastStateSummary() (StateSummary, error) {
	if vm.GetLastStateSummaryF != nil {
		return vm.GetLastStateSummaryF()
	}
	if vm.CantGetLastStateSummary && vm.T != nil {
		vm.T.Fatal(errGetLastStateSummary)
	}
	return nil, errGetLastStateSummary
}

func (vm *TestStateSyncableVM) ParseStateSummary(summaryBytes []byte) (StateSummary, error) {
	if vm.ParseStateSummaryF != nil {
		return vm.ParseStateSummaryF(summaryBytes)
	}
	if vm.CantParseStateSummary && vm.T != nil {
		vm.T.Fatal(errParseStateSummary)
	}
	return nil, errParseStateSummary
}

func (vm *TestStateSyncableVM) GetStateSummary(key uint64) (StateSummary, error) {
	if vm.GetStateSummaryF != nil {
		return vm.GetStateSummaryF(key)
	}
	if vm.CantGetStateSummary && vm.T != nil {
		vm.T.Fatal(errGetStateSummary)
	}
	return nil, errGetStateSummary
}
