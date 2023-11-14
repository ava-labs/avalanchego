// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
)

// Returns a new Server that stores at most the last [maxSummaries] summaries.
func NewServer(maxSummaries int) *Server {
	return &Server{
		maxSummaries: maxSummaries,
		summaries:    linkedhashmap.New[uint64, block.StateSummary](),
	}
}

type Server struct {
	maxSummaries int
	summaries    linkedhashmap.LinkedHashmap[uint64, block.StateSummary]
}

func (s *Server) RecordSummary(summary block.StateSummary) {
	s.summaries.Put(summary.Height(), summary)

	if s.summaries.Len() > s.maxSummaries {
		oldestKey, _, _ := s.summaries.Oldest()
		_ = s.summaries.Delete(oldestKey)
	}
}

func (s *Server) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	_, summary, ok := s.summaries.Newest()
	if !ok {
		return nil, database.ErrNotFound
	}
	return summary, nil
}

func (s *Server) GetStateSummary(_ context.Context, height uint64) (block.StateSummary, error) {
	summary, ok := s.summaries.Get(height)
	if !ok {
		return nil, database.ErrNotFound
	}
	return summary, nil
}
