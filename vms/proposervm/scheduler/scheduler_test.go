// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package scheduler

import (
	"context"
	"github.com/ava-labs/avalanchego/utils/logging"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestDelayFromNew(t *testing.T) {
	startTime := time.Now().Add(50 * time.Millisecond)

	msgs := make(chan common.Message, 1)
	sub := func(ctx context.Context) (common.Message, error) {
		select {
		case msg := <-msgs:
			return msg, nil
		case <-ctx.Done():
			return 0, nil
		}
	}
	s, fromVM := New(sub, &logging.NoLog{})
	defer s.Close()
	go s.Dispatch(startTime)

	msgs <- common.PendingTxs
	fromVM.WaitForEvent(context.Background())

	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestDelayFromSetTime(t *testing.T) {
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	msgs := make(chan common.Message, 1)
	sub := func(ctx context.Context) (common.Message, error) {
		select {
		case msg := <-msgs:
			return msg, nil
		case <-ctx.Done():
			return 0, nil
		}
	}
	s, fromVM := New(sub, &logging.NoLog{})
	defer s.Close()
	go s.Dispatch(now)

	s.SetBuildBlockTime(startTime)

	msgs <- common.PendingTxs

	msg, _ := fromVM.WaitForEvent(context.Background())
	require.Equal(t, common.PendingTxs, msg)
	require.LessOrEqual(t, time.Until(startTime), time.Duration(0))
}

func TestReceipt(*testing.T) {
	msgs := make(chan common.Message, 1)
	sub := func(ctx context.Context) (common.Message, error) {
		select {
		case msg := <-msgs:
			return msg, nil
		case <-ctx.Done():
			return 0, nil
		}
	}
	now := time.Now()
	startTime := now.Add(50 * time.Millisecond)

	s, fromVM := New(sub, &logging.NoLog{})
	defer s.Close()
	go s.Dispatch(now)

	msgs <- common.PendingTxs

	s.SetBuildBlockTime(startTime)

	fromVM.WaitForEvent(context.Background())
}
