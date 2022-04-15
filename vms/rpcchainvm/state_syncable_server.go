// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"google.golang.org/protobuf/types/known/emptypb"
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

func (vm *VMServer) StateSyncGetOngoingSummary(
	context.Context,
	*emptypb.Empty,
) (*vmpb.StateSyncGetOngoingSummaryResponse, error) {
	var (
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.StateSyncGetOngoingSummary()
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err == nil {
		summaryID := summary.ID()
		return &vmpb.StateSyncGetOngoingSummaryResponse{
			Key:       summary.Key(),
			SummaryId: summaryID[:],
			Content:   summary.Bytes(),
		}, nil
	}

	return &vmpb.StateSyncGetOngoingSummaryResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncGetLastSummary(
	ctx context.Context,
	empty *emptypb.Empty,
) (*vmpb.StateSyncGetLastSummaryResponse, error) {
	var (
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.StateSyncGetLastSummary()
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err == nil {
		summaryID := summary.ID()
		return &vmpb.StateSyncGetLastSummaryResponse{
			Key:       summary.Key(),
			SummaryId: summaryID[:],
			Content:   summary.Bytes(),
		}, nil
	}

	return &vmpb.StateSyncGetLastSummaryResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncParseSummary(
	ctx context.Context,
	req *vmpb.StateSyncParseSummaryRequest,
) (*vmpb.StateSyncParseSummaryResponse, error) {
	var (
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.StateSyncParseSummary(req.Summary)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err == nil {
		summaryID := summary.ID()
		return &vmpb.StateSyncParseSummaryResponse{
			Key:       summary.Key(),
			SummaryId: summaryID[:],
			Content:   summary.Bytes(),
		}, nil
	}

	return &vmpb.StateSyncParseSummaryResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncGetSummary(
	ctx context.Context,
	req *vmpb.StateSyncGetSummaryRequest,
) (*vmpb.StateSyncGetSummaryResponse, error) {
	var (
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.StateSyncGetSummary(req.Key)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err == nil {
		summaryID := summary.ID()
		return &vmpb.StateSyncGetSummaryResponse{
			Key:       summary.Key(),
			SummaryId: summaryID[:],
			Content:   summary.Bytes(),
		}, nil
	}

	return &vmpb.StateSyncGetSummaryResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSync(ctx context.Context, req *vmpb.StateSyncRequest) (*vmpb.StateSyncResponse, error) {
	var (
		summaries = make([]common.Summary, len(req.Summaries))
		err       error
	)

	if vm.ssVM != nil {
		for i, sum := range req.Summaries {
			var summaryID ids.ID
			summaryID, err = ids.ToID(sum.SummaryId)
			if err != nil {
				return nil, err
			}
			summaries[i] = &Summary{
				key:   sum.Key,
				id:    summaryID,
				bytes: sum.Content,
			}
		}
		err = vm.ssVM.StateSync(summaries)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	return &vmpb.StateSyncResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncGetResult(context.Context, *emptypb.Empty) (*vmpb.StateSyncGetResultResponse, error) {
	var (
		blkID  ids.ID
		height uint64
		err    error
	)

	if vm.ssVM != nil {
		blkID, height, err = vm.ssVM.StateSyncGetResult()
	} else {
		blkID, height, err = ids.Empty, 0, common.ErrStateSyncableVMNotImplemented
	}

	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	return &vmpb.StateSyncGetResultResponse{
		Bytes:    blkID[:],
		Height:   height,
		ErrorMsg: errMsg,
		Err:      errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncSetLastSummaryBlock(
	ctx context.Context,
	req *vmpb.StateSyncSetLastSummaryBlockRequest,
) (*vmpb.StateSyncSetLastSummaryBlockResponse, error) {
	var err error
	if vm.ssVM != nil {
		err = vm.ssVM.StateSyncSetLastSummaryBlock(req.Bytes)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	return &vmpb.StateSyncSetLastSummaryBlockResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}
