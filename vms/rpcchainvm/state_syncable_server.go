// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"

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

func (vm *VMServer) GetOngoingStateSyncSummary(
	context.Context,
	*emptypb.Empty,
) (*vmpb.GetOngoingStateSyncSummaryResponse, error) {
	var (
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetOngoingStateSyncSummary()
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err == nil {
		summaryID := summary.ID()
		return &vmpb.GetOngoingStateSyncSummaryResponse{
			Summary: &vmpb.StateSyncSummary{
				Key:     summary.Key(),
				Id:      summaryID[:],
				Content: summary.Bytes(),
			},
		}, nil
	}

	return &vmpb.GetOngoingStateSyncSummaryResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetLastStateSummary(
	ctx context.Context,
	empty *emptypb.Empty,
) (*vmpb.GetLastStateSummaryResponse, error) {
	var (
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetLastStateSummary()
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetLastStateSummaryResponse{
			Err: errorToErrCode[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetLastStateSummaryResponse{
		Summary: &vmpb.StateSyncSummary{
			Key:     summary.Key(),
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
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.ParseStateSummary(req.Summary)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.ParseStateSummaryResponse{
			Err: errorToErrCode[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.ParseStateSummaryResponse{
		Summary: &vmpb.StateSyncSummary{
			Key:     summary.Key(),
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
		summary common.Summary
		err     error
	)

	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetStateSummary(req.Key)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetStateSummaryResponse{
			Err: errorToErrCode[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetStateSummaryResponse{
		Summary: &vmpb.StateSyncSummary{
			Key:     summary.Key(),
			Id:      summaryID[:],
			Content: summary.Bytes(),
		},
	}, nil
}

func (vm *VMServer) SummaryAccept(_ context.Context, req *vmpb.SummaryAcceptRequest) (*emptypb.Empty, error) {
	if vm.ssVM == nil {
		return &emptypb.Empty{}, nil
	}
	summary, err := vm.ssVM.GetStateSummary(req.Key)
	if err != nil {
		return nil, err
	}
	if err := summary.Accept(); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) SetSyncableStateSummaries(ctx context.Context, req *vmpb.SetSyncableStateSummariesRequest) (*vmpb.SetSyncableStateSummariesResponse, error) {
	var err error
	if vm.ssVM != nil {
		summaries := make([]common.Summary, len(req.Summaries))
		for i, sum := range req.Summaries {
			summaryID, err := ids.ToID(sum.Id)
			if err != nil {
				return nil, err
			}
			summaries[i] = &deprecatedSummaryToBeRemoved{
				key:   sum.Key,
				id:    summaryID,
				bytes: sum.Content,
			}
		}
		err = vm.ssVM.SetSyncableStateSummaries(summaries)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	return &vmpb.SetSyncableStateSummariesResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetStateSyncResult(context.Context, *emptypb.Empty) (*vmpb.GetStateSyncResultResponse, error) {
	var (
		blkID  = ids.Empty
		height = uint64(0)
		err    = common.ErrStateSyncableVMNotImplemented
	)

	if vm.ssVM != nil {
		blkID, height, err = vm.ssVM.GetStateSyncResult()
	}

	return &vmpb.GetStateSyncResultResponse{
		Bytes:  blkID[:],
		Height: height,
		Err:    errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) SetLastStateSummaryBlock(
	ctx context.Context,
	req *vmpb.SetLastStateSummaryBlockRequest,
) (*vmpb.SetLastStateSummaryBlockResponse, error) {
	var err error
	if vm.ssVM != nil {
		err = vm.ssVM.SetLastStateSummaryBlock(req.Bytes)
	} else {
		err = common.ErrStateSyncableVMNotImplemented
	}

	return &vmpb.SetLastStateSummaryBlockResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}
