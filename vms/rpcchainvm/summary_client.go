// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

var _ block.Summary = &SummaryClient{}

// SummaryClient is an implementation of Summary that talks over RPC.
type SummaryClient struct {
	vm *VMClient

	height  uint64
	id      ids.ID
	bytes   []byte
	blockID ids.ID
}

func (s *SummaryClient) Bytes() []byte   { return s.bytes }
func (s *SummaryClient) Height() uint64  { return s.height }
func (s *SummaryClient) ID() ids.ID      { return s.id }
func (s *SummaryClient) BlockID() ids.ID { return s.blockID }

func (s *SummaryClient) Accept() (bool, error) {
	resp, err := s.vm.client.SummaryAccept(
		context.Background(),
		&vmpb.SummaryAcceptRequest{
			Bytes: s.bytes,
		},
	)
	if err != nil {
		return false, err
	}
	return resp.Accepted, errCodeToError[resp.Err]
}
