// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	reply  APIReply
	err    error
	onCall func()
}

func (mc *mockClient) SendRequest(_ context.Context, _ string, _ interface{}, replyIntf interface{}, _ ...rpc.Option) error {
	reply := replyIntf.(*APIReply)
	*reply = mc.reply
	mc.onCall()
	return mc.err
}

func TestNewClient(t *testing.T) {
	require := require.New(t)

	c := NewClient("")
	require.NotNil(c)
}

func TestClient(t *testing.T) {
	require := require.New(t)

	mc := &mockClient{
		reply: APIReply{
			Healthy: true,
		},
		err:    nil,
		onCall: func() {},
	}
	c := &client{
		requester: mc,
	}

	{
		readiness, err := c.Readiness(context.Background(), nil)
		require.NoError(err)
		require.True(readiness.Healthy)
	}

	{
		health, err := c.Health(context.Background(), nil)
		require.NoError(err)
		require.True(health.Healthy)
	}

	{
		liveness, err := c.Liveness(context.Background(), nil)
		require.NoError(err)
		require.True(liveness.Healthy)
	}

	{
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		healthy, err := AwaitHealthy(ctx, c, time.Second, nil)
		cancel()
		require.NoError(err)
		require.True(healthy)
	}

	{
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		healthy, err := AwaitReady(ctx, c, time.Second, nil)
		cancel()
		require.NoError(err)
		require.True(healthy)
	}

	mc.reply.Healthy = false

	{
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Microsecond)
		healthy, err := AwaitHealthy(ctx, c, time.Microsecond, nil)
		cancel()
		require.ErrorIs(err, context.DeadlineExceeded)
		require.False(healthy)
	}

	{
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Microsecond)
		healthy, err := AwaitReady(ctx, c, time.Microsecond, nil)
		cancel()
		require.ErrorIs(err, context.DeadlineExceeded)
		require.False(healthy)
	}

	mc.onCall = func() {
		mc.reply.Healthy = true
	}

	{
		healthy, err := AwaitHealthy(context.Background(), c, time.Microsecond, nil)
		require.NoError(err)
		require.True(healthy)
	}

	mc.reply.Healthy = false
	{
		healthy, err := AwaitReady(context.Background(), c, time.Microsecond, nil)
		require.NoError(err)
		require.True(healthy)
	}
}
