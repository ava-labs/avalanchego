// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ GearRequester = &gearRequester{}

	errMsgTypeNotRegistered = errors.New("message type not registered")
)

type GearRequester interface {
	PushToRequest(msgType message.Op, vdrID ids.ShortID) error
	HasToRequest(msgType message.Op) bool
	PopToRequest(msgType message.Op, numToPop int) []ids.ShortID
	ClearToRequest(msgType message.Op)

	RecordRequested(msgType message.Op, vdrIDRequests []ids.ShortID) error
	CountRequested(msgType message.Op) int
	ConsumeRequested(msgType message.Op, vdrID ids.ShortID) bool
	ClearRequested(msgType message.Op)

	AddFailed(msgType message.Op, vdrID ids.ShortID) error
	GetAllFailed(msgType message.Op) ids.ShortSet
	ClearFailed(msgType message.Op)
}

func NewGearRequester(log logging.Logger, trackedMsgs []message.Op) GearRequester {
	toRequest := make(map[message.Op]*ids.ShortSet)
	requested := make(map[message.Op]*ids.ShortSet)
	failed := make(map[message.Op]*ids.ShortSet)
	for _, msg := range trackedMsgs {
		toRequestSet := ids.NewShortSet(0)
		toRequest[msg] = &toRequestSet

		requestedSet := ids.NewShortSet(0)
		requested[msg] = &requestedSet

		failedSet := ids.NewShortSet(0)
		failed[msg] = &failedSet
	}

	return &gearRequester{
		log:       log,
		toRequest: toRequest,
		requested: requested,
		failed:    failed,
	}
}

type gearRequester struct {
	log       logging.Logger
	toRequest map[message.Op]*ids.ShortSet
	requested map[message.Op]*ids.ShortSet
	failed    map[message.Op]*ids.ShortSet
}

func (gR *gearRequester) PushToRequest(msgType message.Op, vdrID ids.ShortID) error {
	v, ok := gR.toRequest[msgType]
	if !ok {
		return errMsgTypeNotRegistered
	}

	v.Add(vdrID)
	return nil
}

func (gR *gearRequester) HasToRequest(msgType message.Op) bool {
	v, ok := gR.toRequest[msgType]
	if !ok {
		return false
	}

	return v.Len() > 0
}

func (gR *gearRequester) PopToRequest(msgType message.Op, numToPop int) []ids.ShortID {
	res := make([]ids.ShortID, 0)
	setToRequest, ok := gR.toRequest[msgType]
	if !ok {
		return res
	}

	for i := 0; i < numToPop; i++ {
		id, exists := setToRequest.Pop()
		if !exists {
			break
		}
		res = append(res, id)
	}

	return res
}

func (gR *gearRequester) ClearToRequest(msgType message.Op) {
	v, ok := gR.toRequest[msgType]
	if !ok {
		return
	}

	v.Clear()
}

func (gR *gearRequester) RecordRequested(msgType message.Op, vdrIDRequests []ids.ShortID) error {
	v, ok := gR.requested[msgType]
	if !ok {
		return errMsgTypeNotRegistered
	}

	v.Add(vdrIDRequests...)
	return nil
}

func (gR *gearRequester) CountRequested(msgType message.Op) int {
	v, ok := gR.requested[msgType]
	if !ok {
		return 0
	}

	return v.Len()
}

func (gR *gearRequester) ConsumeRequested(msgType message.Op, vdrID ids.ShortID) bool {
	v, ok := gR.requested[msgType]
	if !ok {
		return false
	}

	if !v.Contains(vdrID) {
		gR.log.Debug("Received message %s from %s unexpectedly", msgType.String(), vdrID)
		return false
	}

	v.Remove(vdrID)
	return true
}

func (gR *gearRequester) ClearRequested(msgType message.Op) {
	v, ok := gR.requested[msgType]
	if !ok {
		return
	}

	v.Clear()
}

func (gR *gearRequester) AddFailed(msgType message.Op, vdrID ids.ShortID) error {
	v, ok := gR.failed[msgType]
	if !ok {
		return errMsgTypeNotRegistered
	}

	v.Add(vdrID)
	return nil
}

func (gR *gearRequester) GetAllFailed(msgType message.Op) ids.ShortSet {
	v, ok := gR.failed[msgType]
	if !ok {
		return ids.ShortSet{}
	}

	return *v
}

func (gR *gearRequester) ClearFailed(msgType message.Op) {
	v, ok := gR.failed[msgType]
	if !ok {
		return
	}

	v.Clear()
}
