// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
)

type messageToRemove struct {
	messageType  constants.MsgType // Must always be set
	inMsg        message.InboundMessage
	notification common.Message

	nodeID         ids.ShortID // Must always be set
	requestID      uint32
	received       time.Time // Time this message was received
	deadline       time.Time // Time this message must be responded to
	onDoneHandling func()
}

func (m messageToRemove) doneHandling() {
	if m.onDoneHandling != nil {
		m.onDoneHandling()
	}
}

// IsPeriodic returns true if this message is of a type that is sent on a
// periodic basis.
func (m messageToRemove) IsPeriodic() bool {
	return m.requestID == constants.GossipMsgRequestID ||
		m.messageType == constants.GossipMsg
}
