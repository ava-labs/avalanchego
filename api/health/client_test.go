// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockClient struct {
	reply  APIHealthReply
	err    error
	onCall func()
}

func (mc *mockClient) SendRequest(method string, params interface{}, replyIntf interface{}) error {
	reply := replyIntf.(*APIHealthReply)
	*reply = mc.reply
	mc.onCall()
	return mc.err
}

func TestNewClient(t *testing.T) {
	assert := assert.New(t)

	c := NewClient("", time.Second)
	assert.NotNil(c)
}

func TestClient(t *testing.T) {
	assert := assert.New(t)

	mc := &mockClient{
		reply: APIHealthReply{
			Healthy: true,
		},
		err:    nil,
		onCall: func() {},
	}
	c := client{
		requester: mc,
	}

	{
		readiness, err := c.Readiness()
		assert.NoError(err)
		assert.True(readiness.Healthy)
	}

	{
		health, err := c.Health()
		assert.NoError(err)
		assert.True(health.Healthy)
	}

	{
		liveness, err := c.Liveness()
		assert.NoError(err)
		assert.True(liveness.Healthy)
	}

	{
		_, err := c.AwaitHealthy(0, time.Second)
		assert.Error(err)
	}

	{
		healthy, err := c.AwaitHealthy(1, time.Second)
		assert.NoError(err)
		assert.True(healthy)
	}

	mc.reply.Healthy = false

	{
		healthy, err := c.AwaitHealthy(2, time.Microsecond)
		assert.NoError(err)
		assert.False(healthy)
	}

	mc.onCall = func() {
		mc.reply.Healthy = true
	}

	{
		healthy, err := c.AwaitHealthy(2, time.Microsecond)
		assert.NoError(err)
		assert.True(healthy)
	}
}
