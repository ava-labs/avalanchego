// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestDelayFromNew(t *testing.T) {
	startTime := time.Now().Add(50 * time.Millisecond)

	msgs := make(chan common.Message, 1)
	sub := func(ctx context.Context, pChainHeight uint64) (common.Message, uint64) {
		return <-msgs, 0
	}
	s, fromVM := New(logging.NoLog{}, sub)
	defer s.Close()
	go s.Dispatch(startTime)

	msgs <- common.PendingTxs
	fromVM.SubscribeToEvents(context.Background(), 0)

	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestDelayFromSetTime(t *testing.T) {
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	msgs := make(chan common.Message, 1)
	sub := func(ctx context.Context, pChainHeight uint64) (common.Message, uint64) {
		return <-msgs, 0
	}
	s, fromVM := New(logging.NoLog{}, sub)
	defer s.Close()
	go s.Dispatch(now)

	s.SetBuildBlockTime(startTime)

	msgs <- common.PendingTxs

	fromVM.SubscribeToEvents(context.Background(), 0)
	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestReceipt(*testing.T) {
	msgs := make(chan common.Message, 1)
	sub := func(ctx context.Context, pChainHeight uint64) (common.Message, uint64) {
		return <-msgs, 0
	}
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(logging.NoLog{}, sub)
	defer s.Close()
	go s.Dispatch(now)

	msgs <- common.PendingTxs

	s.SetBuildBlockTime(startTime)

	fromVM.SubscribeToEvents(context.Background(), 0)
}
