// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

func (vm *VMClient) StateSyncEnabled() (bool, error) {
	resp, err := vm.client.StateSyncEnabled(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return false, err
	}
	err = errCodeToError[resp.Err]
	if err == block.ErrStateSyncableVMNotImplemented {
		return false, nil
	}
	return resp.Enabled, errCodeToError[resp.Err]
}

func (vm *VMClient) GetOngoingSyncStateSummary() (block.StateSummary, error) {
	resp, err := vm.client.GetOngoingSyncStateSummary(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &SummaryClient{
		vm:     vm,
		height: resp.Summary.Height,
		id:     summaryID,
		bytes:  resp.Summary.Content,
	}, err
}

func (vm *VMClient) GetLastStateSummary() (block.StateSummary, error) {
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
	return &SummaryClient{
		vm:     vm,
		height: resp.Summary.Height,
		id:     summaryID,
		bytes:  resp.Summary.Content,
	}, err
}

func (vm *VMClient) ParseStateSummary(summaryBytes []byte) (block.StateSummary, error) {
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
	return &SummaryClient{
		vm:     vm,
		height: resp.Summary.Height,
		id:     summaryID,
		bytes:  resp.Summary.Content,
	}, err
}

func (vm *VMClient) GetStateSummary(height uint64) (block.StateSummary, error) {
	resp, err := vm.client.GetStateSummary(
		context.Background(),
		&vmpb.GetStateSummaryRequest{
			Height: height,
		},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Summary.Id)
	return &SummaryClient{
		vm:     vm,
		height: resp.Summary.Height,
		id:     summaryID,
		bytes:  resp.Summary.Content,
	}, err
}
