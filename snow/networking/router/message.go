// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
)

type message struct {
	messageType  constants.MsgType
	validatorID  ids.ShortID
	requestID    uint32
	containerID  ids.ID
	container    []byte
	containers   [][]byte
	containerIDs ids.Set
	notification common.Message
	received     time.Time // Time this message was received
	deadline     time.Time // Time this message must be responded to
}

func (m message) IsPeriodic() bool {
	return m.requestID == constants.GossipMsgRequestID ||
		m.messageType == constants.GossipMsg
}

func (m message) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("\n    messageType: %s", m.messageType))
	sb.WriteString(fmt.Sprintf("\n    validatorID: %s", m.validatorID))
	sb.WriteString(fmt.Sprintf("\n    requestID: %d", m.requestID))
	switch m.messageType {
	case constants.GetAcceptedMsg, constants.AcceptedMsg, constants.ChitsMsg:
		sb.WriteString(fmt.Sprintf("\n    containerIDs: %s", m.containerIDs))
	case constants.GetMsg, constants.GetAncestorsMsg, constants.PutMsg, constants.PushQueryMsg, constants.PullQueryMsg:
		sb.WriteString(fmt.Sprintf("\n    containerID: %s", m.containerID))
	case constants.MultiPutMsg:
		sb.WriteString(fmt.Sprintf("\n    numContainers: %d", len(m.containers)))
	case constants.NotifyMsg:
		sb.WriteString(fmt.Sprintf("\n    notification: %s", m.notification))
	}
	if !m.deadline.IsZero() {
		sb.WriteString(fmt.Sprintf("\n    deadline: %s", m.deadline))
	}
	return sb.String()
}
