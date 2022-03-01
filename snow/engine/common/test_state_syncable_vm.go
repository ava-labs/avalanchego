// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"
	"testing"
)

var (
	_ StateSyncableVM = &TestStateSyncableVM{}

	errStateSyncEnabled        = errors.New("unexpectedly called StateSyncEnabled")
	errStateSyncGetLastSummary = errors.New("unexpectedly called StateSyncGetLastSummary")
	errStateSyncGetKeyHash     = errors.New("unexpectedly called StateSyncGetKeyHash")
	errStateSyncGetSummary     = errors.New("unexpectedly called StateSyncGetSummary")
	errStateSync               = errors.New("unexpectedly called StateSync")
)

type TestStateSyncableVM struct {
	T *testing.T

	CantStateSyncEnabled, CantStateSyncGetLastSummary,
	CantStateSyncGetKeyHash, CantStateSyncGetSummary,
	CantStateSync bool

	StateSyncEnabledF        func() (bool, error)
	StateSyncGetLastSummaryF func() (Summary, error)
	StateSyncGetKeyHashF     func(Summary) (SummaryKey, SummaryHash, error)
	StateSyncGetSummaryF     func(SummaryKey) (Summary, error)
	StateSyncF               func([]Summary) error
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

func (tss *TestStateSyncableVM) StateSyncGetLastSummary() (Summary, error) {
	if tss.StateSyncGetLastSummaryF != nil {
		return tss.StateSyncGetLastSummaryF()
	}
	if tss.CantStateSyncGetLastSummary && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetLastSummary")
	}
	return nil, errStateSyncGetLastSummary
}

func (tss *TestStateSyncableVM) StateSyncGetKeyHash(summary Summary) (SummaryKey, SummaryHash, error) {
	if tss.StateSyncGetKeyHashF != nil {
		return tss.StateSyncGetKeyHashF(summary)
	}
	if tss.CantStateSyncGetKeyHash && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetKeyHash")
	}
	return nil, nil, errStateSyncGetKeyHash
}

func (tss *TestStateSyncableVM) StateSyncGetSummary(key SummaryKey) (Summary, error) {
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
