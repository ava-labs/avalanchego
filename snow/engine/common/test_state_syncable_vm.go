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
	errStateSyncGetLastSummary    = errors.New("unexpectedly called StateSyncGetLastSummary")
	errStateSyncParseSummary      = errors.New("unexpectedly called StateSyncParseSummary")
	errStateSyncGetSummary        = errors.New("unexpectedly called StateSyncGetSummary")
	errStateSync                  = errors.New("unexpectedly called StateSync")
	errStateSyncGetOngoingSummary = errors.New("unexpectedly called StateSyncGetOngoingSummary")
)

type TestStateSyncableVM struct {
	T *testing.T

	CantStateSyncEnabled, CantStateSyncGetOngoingSummary,
	CantStateSyncGetLastSummary, CantStateSyncParseSummary,
	CantStateSyncGetSummary, CantStateSync bool

	StateSyncEnabledF           func() (bool, error)
	StateSyncGetOngoingSummaryF func() (Summary, error)
	StateSyncGetLastSummaryF    func() (Summary, error)
	StateSyncParseSummaryF      func(summaryBytes []byte) (Summary, error)
	StateSyncGetSummaryF        func(uint64) (Summary, error)
	StateSyncF                  func([]Summary) error
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

func (tss *TestStateSyncableVM) StateSyncGetOngoingSummary() (Summary, error) {
	if tss.StateSyncGetOngoingSummaryF != nil {
		return tss.StateSyncGetOngoingSummaryF()
	}
	if tss.CantStateSyncGetOngoingSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetOngoingSummary")
	}
	return nil, errStateSyncGetOngoingSummary
}

func (tss *TestStateSyncableVM) StateSyncGetLastSummary() (Summary, error) {
	if tss.StateSyncGetLastSummaryF != nil {
		return tss.StateSyncGetLastSummaryF()
	}
	if tss.CantStateSyncGetLastSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetLastSummary")
	}
	return nil, errStateSyncGetLastSummary
}

func (tss *TestStateSyncableVM) StateSyncParseSummary(summaryBytes []byte) (Summary, error) {
	if tss.StateSyncParseSummaryF != nil {
		return tss.StateSyncParseSummaryF(summaryBytes)
	}
	if tss.CantStateSyncParseSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncParseSummary")
	}
	return nil, errStateSyncParseSummary
}

func (tss *TestStateSyncableVM) StateSyncGetSummary(key uint64) (Summary, error) {
	if tss.StateSyncGetSummaryF != nil {
		return tss.StateSyncGetSummaryF(key)
	}
	if tss.CantStateSyncGetSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetSummary")
	}
	return nil, errStateSyncGetSummary
}

func (tss *TestStateSyncableVM) StateSync(summaries []Summary) error {
	if tss.StateSyncF != nil {
		return tss.StateSyncF(summaries)
	}
	if tss.CantStateSync && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSync")
	}
	return errStateSync
}
