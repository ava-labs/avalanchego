// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
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

func (vm *VMClient) GetOngoingStateSyncSummary() (common.Summary, error) {
	resp, err := vm.client.GetOngoingStateSyncSummary(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &Summary{
		key:   resp.Summary.Key,
		id:    summaryID,
		bytes: resp.Summary.Content,
	}, err
}

func (vm *VMClient) GetLastStateSummary() (common.Summary, error) {
	resp, err := vm.client.GetLastStateSummary(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &Summary{
		key:   resp.Summary.Key,
		id:    summaryID,
		bytes: resp.Summary.Content,
	}, err
}

func (vm *VMClient) ParseStateSummary(summaryBytes []byte) (common.Summary, error) {
	resp, err := vm.client.ParseStateSummary(
		context.Background(),
		&vmpb.ParseStateSummaryRequest{
			Summary: summaryBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &Summary{
		key:   resp.Summary.Key,
		id:    summaryID,
		bytes: resp.Summary.Content,
	}, err
}

func (vm *VMClient) GetStateSummary(key uint64) (common.Summary, error) {
	req := &vmpb.GetStateSummaryRequest{Key: key}
	resp, err := vm.client.GetStateSummary(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &Summary{
		key:   resp.Summary.Key,
		id:    summaryID,
		bytes: resp.Summary.Content,
	}, err
}

func (vm *VMClient) SetSyncableStateSummaries(accepted []common.Summary) error {
	requestedSummaries := make([]*vmpb.StateSyncSummary, len(accepted))
	for i, sum := range accepted {
		summaryID := sum.ID()
		requestedSummaries[i] = &vmpb.StateSyncSummary{
			Key:     sum.Key(),
			Id:      summaryID[:],
			Content: sum.Bytes(),
		}
	}
	resp, err := vm.client.SetSyncableStateSummaries(
		context.Background(),
		&vmpb.SetSyncableStateSummariesRequest{
			Summaries: requestedSummaries,
		},
	)
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

func (vm *VMClient) GetStateSyncResult() (ids.ID, uint64, error) {
	resp, err := vm.client.GetStateSyncResult(context.Background(), &emptypb.Empty{})
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

func (vm *VMClient) SetLastStateSummaryBlock(blkBytes []byte) error {
	resp, err := vm.client.SetLastStateSummaryBlock(context.Background(),
		&vmpb.SetLastStateSummaryBlockRequest{
			Bytes: blkBytes,
		})
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}
