// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var errMissingField = errors.New("message missing field")

type chainIDGetter interface {
	GetChainId() []byte
}

func GetChainID(m any) (ids.ID, error) {
	msg, ok := m.(chainIDGetter)
	if !ok {
		return ids.Empty, errMissingField
	}
	chainIDBytes := msg.GetChainId()
	return ids.ToID(chainIDBytes)
}

type sourceChainIDGetter interface {
	GetSourceChainID() ids.ID
}

func GetSourceChainID(m any) (ids.ID, error) {
	msg, ok := m.(sourceChainIDGetter)
	if !ok {
		return GetChainID(m)
	}
	return msg.GetSourceChainID(), nil
}

type requestIDGetter interface {
	GetRequestId() uint32
}

func GetRequestID(m any) (uint32, bool) {
	if msg, ok := m.(requestIDGetter); ok {
		requestID := msg.GetRequestId()
		return requestID, true
	}

	// AppGossip is the only message currently not containing a requestID
	// Here we assign the requestID already in use for gossiped containers
	// to allow a uniform handling of all messages
	if _, ok := m.(*p2ppb.AppGossip); ok {
		return constants.GossipMsgRequestID, true
	}

	return 0, false
}

type deadlineGetter interface {
	GetDeadline() uint64
}

func GetDeadline(m any) (time.Duration, bool) {
	msg, ok := m.(deadlineGetter)
	if !ok {
		return 0, false
	}
	deadline := msg.GetDeadline()
	return time.Duration(deadline), true
}
