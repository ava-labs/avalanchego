// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"

	ids "github.com/ava-labs/avalanchego/ids"
	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (vm *VMClient) StateSyncEnabled() (bool, error) {
	resp, err := vm.client.StateSyncEnabled(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return false, err
	}
	return resp.Enabled, errCodeToError[resp.Err]
}

func (vm *VMClient) StateSyncGetOngoingSummary() (common.Summary, error) {
	resp, err := vm.client.StateSyncGetOngoingSummary(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.SummaryId)
	return &Summary{
		key:   resp.Key,
		id:    summaryID,
		bytes: resp.Content,
	}, err
}

func (vm *VMClient) StateSyncGetLastSummary() (common.Summary, error) {
	resp, err := vm.client.StateSyncGetLastSummary(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.SummaryId)
	return &Summary{
		key:   resp.Key,
		id:    summaryID,
		bytes: resp.Content,
	}, err
}

func (vm *VMClient) StateSyncParseSummary(summaryBytes []byte) (common.Summary, error) {
	resp, err := vm.client.StateSyncParseSummary(
		context.Background(),
		&vmpb.StateSyncParseSummaryRequest{
			Summary: summaryBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.SummaryId)
	return &Summary{
		key:   resp.Key,
		id:    summaryID,
		bytes: resp.Content,
	}, err
}

func (vm *VMClient) StateSyncGetSummary(key uint64) (common.Summary, error) {
	req := &vmpb.StateSyncGetSummaryRequest{Key: key}
	resp, err := vm.client.StateSyncGetSummary(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.SummaryId)
	return &Summary{
		key:   resp.Key,
		id:    summaryID,
		bytes: resp.Content,
	}, err
}

func (vm *VMClient) StateSync(accepted []common.Summary) error {
	requestedSummaries := make([]*vmpb.StateSyncGetLastSummaryResponse, len(accepted))
	for i, sum := range accepted {
		summaryID := sum.ID()
		requestedSummaries[i] = &vmpb.StateSyncGetLastSummaryResponse{
			Key:       sum.Key(),
			SummaryId: summaryID[:],
			Content:   sum.Bytes(),
		}
	}
	resp, err := vm.client.StateSync(
		context.Background(),
		&vmpb.StateSyncRequest{
			Summaries: requestedSummaries,
		},
	)
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

func (vm *VMClient) StateSyncGetResult() (ids.ID, uint64, error) {
	resp, err := vm.client.StateSyncGetResult(context.Background(), &emptypb.Empty{})
	if err != nil {
		return ids.Empty, 0, err
	}
	if errCode := resp.Err; errCode != 0 {
		return ids.Empty, 0, errCodeToError[errCode]
	}

	blkID, err := ids.ToID(resp.Bytes)
	if err != nil {
		return ids.Empty, 0, err
	}

	height := resp.Height
	if len(resp.ErrorMsg) != 0 {
		err = fmt.Errorf(resp.ErrorMsg)
	}
	return blkID, height, err
}

func (vm *VMClient) StateSyncSetLastSummaryBlock(blkByte []byte) error {
	resp, err := vm.client.StateSyncSetLastSummaryBlock(context.Background(),
		&vmpb.StateSyncSetLastSummaryBlockRequest{
			Bytes: blkByte,
		})
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}
