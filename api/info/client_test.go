// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	reply  IsBootstrappedResponse
	err    error
	onCall func()
}

func (mc *mockClient) SendRequest(_ context.Context, _ string, _ interface{}, replyIntf interface{}, _ ...rpc.Option) error {
	reply := replyIntf.(*IsBootstrappedResponse)
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
		reply:  IsBootstrappedResponse{true},
		err:    nil,
		onCall: func() {},
	}
	c := &Client{
		Requester: mc,
	}

	{
		bootstrapped, err := c.IsBootstrapped(t.Context(), "X")
		require.NoError(err)
		require.True(bootstrapped)
	}

	mc.reply.IsBootstrapped = false

	{
		bootstrapped, err := c.IsBootstrapped(t.Context(), "X")
		require.NoError(err)
		require.False(bootstrapped)
	}

	mc.onCall = func() {
		mc.reply.IsBootstrapped = true
	}

	{
		bootstrapped, err := AwaitBootstrapped(t.Context(), c, "X", time.Microsecond)
		require.NoError(err)
		require.True(bootstrapped)
	}
}
