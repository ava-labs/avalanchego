// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var _ snowman.StateSyncableBlock = &StateSyncableBlockClient{}

// SummaryClient is an implementation of Summary that talks over RPC.
type StateSyncableBlockClient struct {
	BlockClient
}

func (b *StateSyncableBlockClient) Register() error {
	b.status = choices.Accepted
	_, err := b.vm.client.StateSyncableBlockRegister(
		context.Background(),
		&vmpb.StateSyncableBlockRegisterRequest{
			Bytes: b.Bytes(),
		},
	)
	return err
}
