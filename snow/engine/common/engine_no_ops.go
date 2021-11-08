// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// EngineNoOps lists all messages that are dropped during nomal operation phase
// Whenever we drop a message, we do that raising an error (unlike BootstrapNoOps)

type EngineNoOps struct {
	Ctx *snow.Context
}

func (nop *EngineNoOps) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedFrontier(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message GetAcceptedFrontier should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Ctx.Log.Debug("AcceptedFrontier(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message AcceptedFrontier should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedFrontierFailed(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message GetAcceptedFrontierFailed should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Ctx.Log.Debug("GetAccepted(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message GetAccepted should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Ctx.Log.Debug("Accepted(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message Accepted should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedFailed(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message GetAcceptedFailed should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	nop.Ctx.Log.Debug("MultiPut(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message MultiPut should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAncestorsFailed(%s, %d) unhandled by engine. Dropped.", validatorID, requestID)
	return fmt.Errorf("message GetAncestorsFailed should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) Halt() {
	nop.Ctx.Log.Debug("Halt unhandled by engine. Dropped.")
}

func (nop *EngineNoOps) Timeout() error {
	nop.Ctx.Log.Debug("Timeout unhandled by engine. Dropped.")
	return fmt.Errorf("message Timeout should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) Connected(validatorID ids.ShortID) error {
	nop.Ctx.Log.Debug("Connected(%s) unhandled by engine. Dropped.", validatorID)
	return fmt.Errorf("message Connected should not be handled by engine. Dropping it")
}

func (nop *EngineNoOps) Disconnected(validatorID ids.ShortID) error {
	nop.Ctx.Log.Debug("Disconnected(%s) unhandled by engine. Dropped.", validatorID)
	return fmt.Errorf("message Disconnected should not be handled by engine. Dropping it")
}
