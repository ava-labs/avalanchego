// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestBlockingMessageQueue(t *testing.T) {
	assert := assert.New(t)

	q := NewBlockingMessageQueue(
		SendFailedFunc(func(msg message.OutboundMessage) {
			t.Fail()
		}),
		logging.NoLog{},
		0,
	)

	mc := newMessageCreator(t)
	msg, err := mc.Ping()
	assert.NoError(err)

	numToSend := 10
	go func() {
		for i := 0; i < numToSend; i++ {
			q.Push(context.Background(), msg)
		}
	}()

	for i := 0; i < numToSend; i++ {
		_, ok := q.Pop()
		assert.True(ok)
	}

	_, ok := q.PopNow()
	assert.False(ok)

	q.Close()

	_, ok = q.Pop()
	assert.False(ok)
}
