// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "fmt"

// MsgType ...
type MsgType int

const (
	NullMsg MsgType = iota
	GetAcceptedFrontierMsg
	AcceptedFrontierMsg
	GetAcceptedFrontierFailedMsg
	GetAcceptedMsg
	AcceptedMsg
	GetAcceptedFailedMsg
	GetMsg
	PutMsg
	GetFailedMsg
	PushQueryMsg
	PullQueryMsg
	ChitsMsg
	QueryFailedMsg
	ConnectedMsg
	DisconnectedMsg
	NotifyMsg
	GossipMsg
	GetAncestorsMsg
	MultiPutMsg
	GetAncestorsFailedMsg
)

func (t MsgType) String() string {
	switch t {
	case NullMsg:
		return "Null Message"
	case GetAcceptedFrontierMsg:
		return "Get Accepted Frontier Message"
	case AcceptedFrontierMsg:
		return "Accepted Frontier Message"
	case GetAcceptedFrontierFailedMsg:
		return "Get Accepted Frontier Failed Message"
	case GetAcceptedMsg:
		return "Get Accepted Message"
	case AcceptedMsg:
		return "Accepted Message"
	case GetAcceptedFailedMsg:
		return "Get Accepted Failed Message"
	case GetMsg:
		return "Get Message"
	case GetAncestorsMsg:
		return "Get Ancestors Message"
	case GetAncestorsFailedMsg:
		return "Get Ancestors Failed Message"
	case PutMsg:
		return "Put Message"
	case MultiPutMsg:
		return "MultiPut Message"
	case GetFailedMsg:
		return "Get Failed Message"
	case PushQueryMsg:
		return "Push Query Message"
	case PullQueryMsg:
		return "Pull Query Message"
	case ChitsMsg:
		return "Chits Message"
	case QueryFailedMsg:
		return "Query Failed Message"
	case ConnectedMsg:
		return "Connected Message"
	case DisconnectedMsg:
		return "Disconnected Message"
	case NotifyMsg:
		return "Notify Message"
	case GossipMsg:
		return "Gossip Message"
	default:
		return fmt.Sprintf("Unknown Message Type: %d", t)
	}
}
