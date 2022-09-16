// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestBlockingMessageQueue(t *testing.T) {
	require := require.New(t)

	for _, useProto := range []bool{false, true} {
		t.Run(fmt.Sprintf("use proto buf message creator %v", useProto), func(tt *testing.T) {
			q := NewBlockingMessageQueue(
				SendFailedFunc(func(msg message.OutboundMessage) {
					t.Fail()
				}),
				logging.NoLog{},
				0,
			)

			mc, mcProto := newMessageCreator(tt)

			var (
				msg message.OutboundMessage
				err error
			)
			if useProto {
				msg, err = mcProto.Ping()
			} else {
				msg, err = mc.Ping()
			}
			require.NoError(err)

			numToSend := 10
			go func() {
				for i := 0; i < numToSend; i++ {
					q.Push(context.Background(), msg)
				}
			}()

			for i := 0; i < numToSend; i++ {
				_, ok := q.Pop()
				require.True(ok)
			}

			_, ok := q.PopNow()
			require.False(ok)

			q.Close()

			_, ok = q.Pop()
			require.False(ok)
		})
	}
}
