// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Server struct{}

func (s *Server) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}

func (s *Server) GetStateSummary(context.Context, uint64) (block.StateSummary, error) {
	return nil, errors.New("TODO")
}
