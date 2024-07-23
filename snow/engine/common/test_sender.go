// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package common

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ Sender    = (*SenderTest)(nil)
	_ AppSender = (*FakeSender)(nil)

	errSendAppRequest  = errors.New("unexpectedly called SendAppRequest")
	errSendAppResponse = errors.New("unexpectedly called SendAppResponse")
	errSendAppError    = errors.New("unexpectedly called SendAppError")
	errSendAppGossip   = errors.New("unexpectedly called SendAppGossip")
)

// SenderTest is a test sender
type SenderTest struct {
	T require.TestingT

	CantSendGetStateSummaryFrontier, CantSendStateSummaryFrontier,
	CantSendGetAcceptedStateSummary, CantSendAcceptedStateSummary,
	CantSendGetAcceptedFrontier, CantSendAcceptedFrontier,
	CantSendGetAccepted, CantSendAccepted,
	CantSendGet, CantSendGetAncestors, CantSendPut, CantSendAncestors,
	CantSendPullQuery, CantSendPushQuery, CantSendChits,
	CantSendAppRequest, CantSendAppResponse, CantSendAppError,
	CantSendAppGossip,
	CantSendCrossChainAppRequest, CantSendCrossChainAppResponse, CantSendCrossChainAppError bool

	SendGetStateSummaryFrontierF func(context.Context, set.Set[ids.NodeID], uint32)
	SendStateSummaryFrontierF    func(context.Context, ids.NodeID, uint32, []byte)
	SendGetAcceptedStateSummaryF func(context.Context, set.Set[ids.NodeID], uint32, []uint64)
	SendAcceptedStateSummaryF    func(context.Context, ids.NodeID, uint32, []ids.ID)
	SendGetAcceptedFrontierF     func(context.Context, set.Set[ids.NodeID], uint32)
	SendAcceptedFrontierF        func(context.Context, ids.NodeID, uint32, ids.ID)
	SendGetAcceptedF             func(context.Context, set.Set[ids.NodeID], uint32, []ids.ID)
	SendAcceptedF                func(context.Context, ids.NodeID, uint32, []ids.ID)
	SendGetF                     func(context.Context, ids.NodeID, uint32, ids.ID)
	SendGetAncestorsF            func(context.Context, ids.NodeID, uint32, ids.ID)
	SendPutF                     func(context.Context, ids.NodeID, uint32, []byte)
	SendAncestorsF               func(context.Context, ids.NodeID, uint32, [][]byte)
	SendPushQueryF               func(context.Context, set.Set[ids.NodeID], uint32, []byte, uint64)
	SendPullQueryF               func(context.Context, set.Set[ids.NodeID], uint32, ids.ID, uint64)
	SendChitsF                   func(context.Context, ids.NodeID, uint32, ids.ID, ids.ID, ids.ID)
	SendAppRequestF              func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF             func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF                func(context.Context, ids.NodeID, uint32, int32, string) error
	SendAppGossipF               func(context.Context, SendConfig, []byte) error
	SendCrossChainAppRequestF    func(context.Context, ids.ID, uint32, []byte)
	SendCrossChainAppResponseF   func(context.Context, ids.ID, uint32, []byte)
	SendCrossChainAppErrorF      func(context.Context, ids.ID, uint32, int32, string)
}

// Default set the default callable value to [cant]
func (s *SenderTest) Default(cant bool) {
	s.CantSendGetStateSummaryFrontier = cant
	s.CantSendStateSummaryFrontier = cant
	s.CantSendGetAcceptedStateSummary = cant
	s.CantSendAcceptedStateSummary = cant
	s.CantSendGetAcceptedFrontier = cant
	s.CantSendAcceptedFrontier = cant
	s.CantSendGetAccepted = cant
	s.CantSendAccepted = cant
	s.CantSendGet = cant
	s.CantSendGetAccepted = cant
	s.CantSendPut = cant
	s.CantSendAncestors = cant
	s.CantSendPullQuery = cant
	s.CantSendPushQuery = cant
	s.CantSendChits = cant
	s.CantSendAppRequest = cant
	s.CantSendAppResponse = cant
	s.CantSendAppGossip = cant
	s.CantSendCrossChainAppRequest = cant
	s.CantSendCrossChainAppResponse = cant
}

// SendGetStateSummaryFrontier calls SendGetStateSummaryFrontierF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendGetStateSummaryFrontier(ctx context.Context, validatorIDs set.Set[ids.NodeID], requestID uint32) {
	if s.SendGetStateSummaryFrontierF != nil {
		s.SendGetStateSummaryFrontierF(ctx, validatorIDs, requestID)
	} else if s.CantSendGetStateSummaryFrontier && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendGetStateSummaryFrontier")
	}
}

// SendStateSummaryFrontier calls SendStateSummaryFrontierF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendStateSummaryFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32, summary []byte) {
	if s.SendStateSummaryFrontierF != nil {
		s.SendStateSummaryFrontierF(ctx, validatorID, requestID, summary)
	} else if s.CantSendStateSummaryFrontier && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendStateSummaryFrontier")
	}
}

// SendGetAcceptedStateSummary calls SendGetAcceptedStateSummaryF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendGetAcceptedStateSummary(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, heights []uint64) {
	if s.SendGetAcceptedStateSummaryF != nil {
		s.SendGetAcceptedStateSummaryF(ctx, nodeIDs, requestID, heights)
	} else if s.CantSendGetAcceptedStateSummary && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendGetAcceptedStateSummaryF")
	}
}

// SendAcceptedStateSummary calls SendAcceptedStateSummaryF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendAcceptedStateSummary(ctx context.Context, validatorID ids.NodeID, requestID uint32, summaryIDs []ids.ID) {
	if s.SendAcceptedStateSummaryF != nil {
		s.SendAcceptedStateSummaryF(ctx, validatorID, requestID, summaryIDs)
	} else if s.CantSendAcceptedStateSummary && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendAcceptedStateSummary")
	}
}

// SendGetAcceptedFrontier calls SendGetAcceptedFrontierF if it was initialized.
// If it wasn't initialized and this function shouldn't be called and testing
// was initialized, then testing will fail.
func (s *SenderTest) SendGetAcceptedFrontier(ctx context.Context, validatorIDs set.Set[ids.NodeID], requestID uint32) {
	if s.SendGetAcceptedFrontierF != nil {
		s.SendGetAcceptedFrontierF(ctx, validatorIDs, requestID)
	} else if s.CantSendGetAcceptedFrontier && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendGetAcceptedFrontier")
	}
}

// SendAcceptedFrontier calls SendAcceptedFrontierF if it was initialized. If it
// wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAcceptedFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32, containerID ids.ID) {
	if s.SendAcceptedFrontierF != nil {
		s.SendAcceptedFrontierF(ctx, validatorID, requestID, containerID)
	} else if s.CantSendAcceptedFrontier && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendAcceptedFrontier")
	}
}

// SendGetAccepted calls SendGetAcceptedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendGetAccepted(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerIDs []ids.ID) {
	if s.SendGetAcceptedF != nil {
		s.SendGetAcceptedF(ctx, nodeIDs, requestID, containerIDs)
	} else if s.CantSendGetAccepted && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendGetAccepted")
	}
}

// SendAccepted calls SendAcceptedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAccepted(ctx context.Context, validatorID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	if s.SendAcceptedF != nil {
		s.SendAcceptedF(ctx, validatorID, requestID, containerIDs)
	} else if s.CantSendAccepted && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendAccepted")
	}
}

// SendGet calls SendGetF if it was initialized. If it wasn't initialized and
// this function shouldn't be called and testing was initialized, then testing
// will fail.
func (s *SenderTest) SendGet(ctx context.Context, vdr ids.NodeID, requestID uint32, containerID ids.ID) {
	if s.SendGetF != nil {
		s.SendGetF(ctx, vdr, requestID, containerID)
	} else if s.CantSendGet && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendGet")
	}
}

// SendGetAncestors calls SendGetAncestorsF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendGetAncestors(ctx context.Context, validatorID ids.NodeID, requestID uint32, containerID ids.ID) {
	if s.SendGetAncestorsF != nil {
		s.SendGetAncestorsF(ctx, validatorID, requestID, containerID)
	} else if s.CantSendGetAncestors && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendCantSendGetAncestors")
	}
}

// SendPut calls SendPutF if it was initialized. If it wasn't initialized and
// this function shouldn't be called and testing was initialized, then testing
// will fail.
func (s *SenderTest) SendPut(ctx context.Context, vdr ids.NodeID, requestID uint32, container []byte) {
	if s.SendPutF != nil {
		s.SendPutF(ctx, vdr, requestID, container)
	} else if s.CantSendPut && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendPut")
	}
}

// SendAncestors calls SendAncestorsF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAncestors(ctx context.Context, vdr ids.NodeID, requestID uint32, containers [][]byte) {
	if s.SendAncestorsF != nil {
		s.SendAncestorsF(ctx, vdr, requestID, containers)
	} else if s.CantSendAncestors && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendAncestors")
	}
}

// SendPushQuery calls SendPushQueryF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendPushQuery(ctx context.Context, vdrs set.Set[ids.NodeID], requestID uint32, container []byte, requestedHeight uint64) {
	if s.SendPushQueryF != nil {
		s.SendPushQueryF(ctx, vdrs, requestID, container, requestedHeight)
	} else if s.CantSendPushQuery && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendPushQuery")
	}
}

// SendPullQuery calls SendPullQueryF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendPullQuery(ctx context.Context, vdrs set.Set[ids.NodeID], requestID uint32, containerID ids.ID, requestedHeight uint64) {
	if s.SendPullQueryF != nil {
		s.SendPullQueryF(ctx, vdrs, requestID, containerID, requestedHeight)
	} else if s.CantSendPullQuery && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendPullQuery")
	}
}

// SendChits calls SendChitsF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *SenderTest) SendChits(ctx context.Context, vdr ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID) {
	if s.SendChitsF != nil {
		s.SendChitsF(ctx, vdr, requestID, preferredID, preferredIDAtHeight, acceptedID)
	} else if s.CantSendChits && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendChits")
	}
}

// SendCrossChainAppRequest calls SendCrossChainAppRequestF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		s.SendCrossChainAppRequestF(ctx, chainID, requestID, appRequestBytes)
	} else if s.CantSendCrossChainAppRequest && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendCrossChainAppRequest")
	}
	return nil
}

// SendCrossChainAppResponse calls SendCrossChainAppResponseF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		s.SendCrossChainAppResponseF(ctx, chainID, requestID, appResponseBytes)
	} else if s.CantSendCrossChainAppResponse && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendCrossChainAppResponse")
	}
	return nil
}

// SendCrossChainAppError calls SendCrossChainAppErrorF if it was
// initialized. If it wasn't initialized and this function shouldn't be called
// and testing was initialized, then testing will fail.
func (s *SenderTest) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendCrossChainAppErrorF != nil {
		s.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	} else if s.CantSendCrossChainAppError && s.T != nil {
		require.FailNow(s.T, "Unexpectedly called SendCrossChainAppError")
	}
	return nil
}

// SendAppRequest calls SendAppRequestF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	switch {
	case s.SendAppRequestF != nil:
		return s.SendAppRequestF(ctx, nodeIDs, requestID, appRequestBytes)
	case s.CantSendAppRequest && s.T != nil:
		require.FailNow(s.T, errSendAppRequest.Error())
	}
	return errSendAppRequest
}

// SendAppResponse calls SendAppResponseF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	switch {
	case s.SendAppResponseF != nil:
		return s.SendAppResponseF(ctx, nodeID, requestID, appResponseBytes)
	case s.CantSendAppResponse && s.T != nil:
		require.FailNow(s.T, errSendAppResponse.Error())
	}
	return errSendAppResponse
}

// SendAppError calls SendAppErrorF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	switch {
	case s.SendAppErrorF != nil:
		return s.SendAppErrorF(ctx, nodeID, requestID, code, message)
	case s.CantSendAppError && s.T != nil:
		require.FailNow(s.T, errSendAppError.Error())
	}
	return errSendAppError
}

// SendAppGossip calls SendAppGossipF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) SendAppGossip(
	ctx context.Context,
	config SendConfig,
	appGossipBytes []byte,
) error {
	switch {
	case s.SendAppGossipF != nil:
		return s.SendAppGossipF(ctx, config, appGossipBytes)
	case s.CantSendAppGossip && s.T != nil:
		require.FailNow(s.T, errSendAppGossip.Error())
	}
	return errSendAppGossip
}

// FakeSender is used for testing
type FakeSender struct {
	SentAppRequest, SentAppResponse,
	SentAppGossip,
	SentCrossChainAppRequest, SentCrossChainAppResponse chan []byte

	SentAppError, SentCrossChainAppError chan *AppError
}

func (f FakeSender) SendAppRequest(_ context.Context, _ set.Set[ids.NodeID], _ uint32, bytes []byte) error {
	if f.SentAppRequest == nil {
		return nil
	}

	f.SentAppRequest <- bytes
	return nil
}

func (f FakeSender) SendAppResponse(_ context.Context, _ ids.NodeID, _ uint32, bytes []byte) error {
	if f.SentAppResponse == nil {
		return nil
	}

	f.SentAppResponse <- bytes
	return nil
}

func (f FakeSender) SendAppError(_ context.Context, _ ids.NodeID, _ uint32, errorCode int32, errorMessage string) error {
	if f.SentAppError == nil {
		return nil
	}

	f.SentAppError <- &AppError{
		Code:    errorCode,
		Message: errorMessage,
	}
	return nil
}

func (f FakeSender) SendAppGossip(_ context.Context, _ SendConfig, bytes []byte) error {
	if f.SentAppGossip == nil {
		return nil
	}

	f.SentAppGossip <- bytes
	return nil
}

func (f FakeSender) SendCrossChainAppRequest(_ context.Context, _ ids.ID, _ uint32, bytes []byte) error {
	if f.SentCrossChainAppRequest == nil {
		return nil
	}

	f.SentCrossChainAppRequest <- bytes
	return nil
}

func (f FakeSender) SendCrossChainAppResponse(_ context.Context, _ ids.ID, _ uint32, bytes []byte) error {
	if f.SentCrossChainAppResponse == nil {
		return nil
	}

	f.SentCrossChainAppResponse <- bytes
	return nil
}

func (f FakeSender) SendCrossChainAppError(_ context.Context, _ ids.ID, _ uint32, errorCode int32, errorMessage string) error {
	if f.SentCrossChainAppError == nil {
		return nil
	}

	f.SentCrossChainAppError <- &AppError{
		Code:    errorCode,
		Message: errorMessage,
	}
	return nil
}
