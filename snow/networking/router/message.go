// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
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
	notifyMsg
	gossipMsg
	shutdownMsg
	getAncestorsMsg
	putAncestorMsg
)

type message struct {
	messageType  msgType
	validatorID  ids.ShortID
	requestID    uint32
	containerID  ids.ID
	container    []byte
	containerIDs ids.Set
	notification common.Message
}

func (m message) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("\n    messageType: %s", m.messageType.String()))
	sb.WriteString(fmt.Sprintf("\n    validatorID: %s", m.validatorID.String()))
	sb.WriteString(fmt.Sprintf("\n    requestID: %d", m.requestID))
	sb.WriteString(fmt.Sprintf("\n    containerID: %s", m.containerID.String()))
	sb.WriteString(fmt.Sprintf("\n    containerIDs: %s", m.containerIDs.String()))
	if m.messageType == notifyMsg {
		sb.WriteString(fmt.Sprintf("\n    notification: %s", m.notification.String()))
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
	case putMsg:
		return "Put Message"
	case putAncestorMsg:
		return "Put Ancestor Message"
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
	case notifyMsg:
		return "Notify Message"
	case gossipMsg:
		return "Gossip Message"
	case shutdownMsg:
		return "Shutdown Message"
	default:
		return fmt.Sprintf("Unknown Message Type: %d", t)
	}
}
