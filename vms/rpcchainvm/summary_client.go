// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.Summary = &SummaryClient{}
	_ common.Summary = &deprecatedSummaryToBeRemoved{}
)

type deprecatedSummaryToBeRemoved struct {
	height uint64
	id     ids.ID
	bytes  []byte
}

func (s *deprecatedSummaryToBeRemoved) Bytes() []byte  { return s.bytes }
func (s *deprecatedSummaryToBeRemoved) Height() uint64 { return s.height }
func (s *deprecatedSummaryToBeRemoved) ID() ids.ID     { return s.id }
func (s *deprecatedSummaryToBeRemoved) Accept() error  { return fmt.Errorf("TO BE IMPLEMENTED") }

// SummaryClient is an implementation of Summary that talks over RPC.
type SummaryClient struct {
	vm *VMClient

	height uint64
	id     ids.ID
	bytes  []byte
}

func (s *SummaryClient) Bytes() []byte  { return s.bytes }
func (s *SummaryClient) Height() uint64 { return s.height }
func (s *SummaryClient) ID() ids.ID     { return s.id }

func (s *SummaryClient) Accept() error {
	_, err := s.vm.client.SummaryAccept(
		context.Background(),
		&vmpb.SummaryAcceptRequest{
			Height: s.height,
		},
	)
	return err
}
