// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

var _ common.Summary = &summary{}

type summary struct {
	key   uint64
	id    ids.ID
	bytes []byte
}

func (s *summary) Bytes() []byte { return s.bytes }
func (s *summary) Key() uint64   { return s.key }
func (s *summary) ID() ids.ID    { return s.id }

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
	return &summary{
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
	return &summary{
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
	return &summary{
		key:   resp.Summary.Key,
		id:    summaryID,
		bytes: resp.Summary.Content,
	}, err
}

func (vm *VMClient) GetStateSummary(key uint64) (common.Summary, error) {
	resp, err := vm.client.GetStateSummary(
		context.Background(),
		&vmpb.GetStateSummaryRequest{
			Key: key,
		},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &summary{
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
