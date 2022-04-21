// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"
)

var (
	_ StateSyncableVM = &TestStateSyncableVM{}

	errStateSyncEnabled           = errors.New("unexpectedly called StateSyncEnabled")
	errGetLastStateSummary        = errors.New("unexpectedly called GetLastStateSummary")
	errParseStateSummary          = errors.New("unexpectedly called ParseStateSummary")
	errGetStateSummary            = errors.New("unexpectedly called GetStateSummary")
	errStateSyncGetOngoingSummary = errors.New("unexpectedly called StateSyncGetOngoingSummary")
)

type TestStateSyncableVM struct {
	T *testing.T

	CantStateSyncEnabled, CantStateSyncGetOngoingSummary,
	CantGetLastStateSummary, CantParseStateSummary,
	CantGetStateSummary bool

	StateSyncEnabledF           func() (bool, error)
	GetOngoingSyncStateSummaryF func() (Summary, error)
	GetLastStateSummaryF        func() (Summary, error)
	ParseStateSummaryF          func(summaryBytes []byte) (Summary, error)
	GetStateSummaryF            func(uint64) (Summary, error)
}

func (tss *TestStateSyncableVM) StateSyncEnabled() (bool, error) {
	if tss.StateSyncEnabledF != nil {
		return tss.StateSyncEnabledF()
	}
	if tss.CantStateSyncEnabled && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncEnabled")
	}
	return false, errStateSyncEnabled
}

func (tss *TestStateSyncableVM) GetOngoingSyncStateSummary() (Summary, error) {
	if tss.GetOngoingSyncStateSummaryF != nil {
		return tss.GetOngoingSyncStateSummaryF()
	}
	if tss.CantStateSyncGetOngoingSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetOngoingSummary")
	}
	return nil, errStateSyncGetOngoingSummary
}

func (tss *TestStateSyncableVM) GetLastStateSummary() (Summary, error) {
	if tss.GetLastStateSummaryF != nil {
		return tss.GetLastStateSummaryF()
	}
	if tss.CantGetLastStateSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called GetLastStateSummary")
	}
	return nil, errGetLastStateSummary
}

func (tss *TestStateSyncableVM) ParseStateSummary(summaryBytes []byte) (Summary, error) {
	if tss.ParseStateSummaryF != nil {
		return tss.ParseStateSummaryF(summaryBytes)
	}
	if tss.CantParseStateSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called ParseStateSummary")
	}
	return nil, errParseStateSummary
}

func (tss *TestStateSyncableVM) GetStateSummary(key uint64) (Summary, error) {
	if tss.GetStateSummaryF != nil {
		return tss.GetStateSummaryF(key)
	}
	if tss.CantGetStateSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called GetStateSummary")
	}
	return nil, errGetStateSummary
}
