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

type msgType int

const (
	nullMsg msgType = iota
	getAcceptedFrontierMsg
	acceptedFrontierMsg
	getAcceptedFrontierFailedMsg
	getAcceptedMsg
	acceptedMsg
	getAcceptedFailedMsg
	getMsg
	putMsg
	getFailedMsg
	pushQueryMsg
	pullQueryMsg
	chitsMsg
	queryFailedMsg
	connectedMsg
	disconnectedMsg
	notifyMsg
	gossipMsg
	getAncestorsMsg
	multiPutMsg
	getAncestorsFailedMsg
)

type message struct {
	messageType  msgType
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
		m.messageType == gossipMsg
}

func (m message) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("\n    messageType: %s", m.messageType))
	sb.WriteString(fmt.Sprintf("\n    validatorID: %s", m.validatorID))
	sb.WriteString(fmt.Sprintf("\n    requestID: %d", m.requestID))
	switch m.messageType {
	case getAcceptedMsg, acceptedMsg, chitsMsg:
		sb.WriteString(fmt.Sprintf("\n    containerIDs: %s", m.containerIDs))
	case getMsg, getAncestorsMsg, putMsg, pushQueryMsg, pullQueryMsg:
		sb.WriteString(fmt.Sprintf("\n    containerID: %s", m.containerID))
	case multiPutMsg:
		sb.WriteString(fmt.Sprintf("\n    numContainers: %d", len(m.containers)))
	case notifyMsg:
		sb.WriteString(fmt.Sprintf("\n    notification: %s", m.notification))
	}
	if !m.deadline.IsZero() {
		sb.WriteString(fmt.Sprintf("\n    deadline: %s", m.deadline))
	}
	return sb.String()
}

func (t msgType) String() string {
	switch t {
	case nullMsg:
		return "Null Message"
	case getAcceptedFrontierMsg:
		return "Get Accepted Frontier Message"
	case acceptedFrontierMsg:
		return "Accepted Frontier Message"
	case getAcceptedFrontierFailedMsg:
		return "Get Accepted Frontier Failed Message"
	case getAcceptedMsg:
		return "Get Accepted Message"
	case acceptedMsg:
		return "Accepted Message"
	case getAcceptedFailedMsg:
		return "Get Accepted Failed Message"
	case getMsg:
		return "Get Message"
	case getAncestorsMsg:
		return "Get Ancestors Message"
	case getAncestorsFailedMsg:
		return "Get Ancestors Failed Message"
	case putMsg:
		return "Put Message"
	case multiPutMsg:
		return "MultiPut Message"
	case getFailedMsg:
		return "Get Failed Message"
	case pushQueryMsg:
		return "Push Query Message"
	case pullQueryMsg:
		return "Pull Query Message"
	case chitsMsg:
		return "Chits Message"
	case queryFailedMsg:
		return "Query Failed Message"
	case connectedMsg:
		return "Connected Message"
	case disconnectedMsg:
		return "Disconnected Message"
	case notifyMsg:
		return "Notify Message"
	case gossipMsg:
		return "Gossip Message"
	default:
		return fmt.Sprintf("Unknown Message Type: %d", t)
	}
}
