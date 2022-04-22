// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

func (vm *VMServer) StateSyncEnabled(context.Context, *emptypb.Empty) (*vmpb.StateSyncEnabledResponse, error) {
	var (
		enabled bool
		err     error
	)

	if vm.ssVM != nil {
		enabled, err = vm.ssVM.StateSyncEnabled()
	}

	return &vmpb.StateSyncEnabledResponse{
		Enabled: enabled,
		Err:     errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetOngoingSyncStateSummary(
	context.Context,
	*emptypb.Empty,
) (*vmpb.GetOngoingSyncStateSummaryResponse, error) {
	var (
		summary block.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetOngoingSyncStateSummary()
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err == nil {
		summaryID := summary.ID()
		return &vmpb.GetOngoingSyncStateSummaryResponse{
			Summary: &vmpb.StateSyncSummary{
				Height:  summary.Height(),
				Id:      summaryID[:],
				Content: summary.Bytes(),
			},
		}, nil
	}

	return &vmpb.GetOngoingSyncStateSummaryResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetLastStateSummary(
	ctx context.Context,
	empty *emptypb.Empty,
) (*vmpb.GetLastStateSummaryResponse, error) {
	var (
		summary block.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetLastStateSummary()
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetLastStateSummaryResponse{
			Err: errorToErrCode[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetLastStateSummaryResponse{
		Summary: &vmpb.StateSyncSummary{
			Height:  summary.Height(),
			Id:      summaryID[:],
			Content: summary.Bytes(),
		},
	}, nil
}

func (vm *VMServer) ParseStateSummary(
	ctx context.Context,
	req *vmpb.ParseStateSummaryRequest,
) (*vmpb.ParseStateSummaryResponse, error) {
	var (
		summary block.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.ParseStateSummary(req.Summary)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.ParseStateSummaryResponse{
			Err: errorToErrCode[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.ParseStateSummaryResponse{
		Summary: &vmpb.StateSyncSummary{
			Height:  summary.Height(),
			Id:      summaryID[:],
			Content: summary.Bytes(),
		},
	}, nil
}

func (vm *VMServer) GetStateSummary(
	ctx context.Context,
	req *vmpb.GetStateSummaryRequest,
) (*vmpb.GetStateSummaryResponse, error) {
	var (
		summary block.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetStateSummary(req.Height)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetStateSummaryResponse{
			Err: errorToErrCode[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetStateSummaryResponse{
		Summary: &vmpb.StateSyncSummary{
			Height:  summary.Height(),
			Id:      summaryID[:],
			Content: summary.Bytes(),
		},
	}, nil
}

func (vm *VMServer) SummaryAccept(
	_ context.Context,
	req *vmpb.SummaryAcceptRequest,
) (*vmpb.SummaryAcceptResponse, error) {
	var (
		accepted bool
		err      error
	)
	if vm.ssVM == nil {
		accepted, err = false, block.ErrStateSyncableVMNotImplemented
	} else {
		var summary block.Summary
		summary, err = vm.ssVM.ParseStateSummary(req.Bytes)
		if err == nil {
			accepted, err = summary.Accept()
		}
	}

	return &vmpb.SummaryAcceptResponse{
		Accepted: accepted,
		Err:      errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetStateSyncResult(context.Context, *emptypb.Empty) (*vmpb.GetStateSyncResultResponse, error) {
	err := block.ErrStateSyncableVMNotImplemented

	if vm.ssVM != nil {
		err = vm.ssVM.GetStateSyncResult()
	}

	return &vmpb.GetStateSyncResultResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}
