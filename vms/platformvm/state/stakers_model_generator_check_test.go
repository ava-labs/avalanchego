// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errNotAStakerTx = errors.New("tx is not a stakerTx")
	errWrongNodeID  = errors.New("unexpected nodeID")
)

// TestGeneratedStakersValidity tests the staker generator itself.
// It documents and verifies the invariants enforced by the staker generator.
func TestGeneratedStakersValidity(t *testing.T) {
	properties := gopter.NewProperties(nil)

	ctx := snowtest.Context(&testing.T{}, snowtest.PChainID)
	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	maxDelegatorWeight := uint64(2023)

	properties.Property("AddValidatorTx generator checks", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx, %w", err))
			}

			if err := signedTx.SyntacticVerify(ctx); err != nil {
				return err.Error()
			}

			addValTx, ok := signedTx.Unsigned.(*txs.AddValidatorTx)
			if !ok {
				return errNotAStakerTx.Error()
			}

			if nodeID != addValTx.NodeID() {
				return errWrongNodeID.Error()
			}

			currentVal, err := NewCurrentStaker(signedTx.ID(), addValTx, addValTx.StartTime(), uint64(100))
			if err != nil {
				return err.Error()
			}

			if currentVal.EndTime.Before(currentVal.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					currentVal.StartTime, currentVal.EndTime, currentVal)
			}

			pendingVal, err := NewPendingStaker(signedTx.ID(), addValTx)
			if err != nil {
				return err.Error()
			}

			if pendingVal.EndTime.Before(pendingVal.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					pendingVal.StartTime, pendingVal.EndTime, pendingVal)
			}

			return ""
		},
		addValidatorTxGenerator(ctx, &nodeID, math.MaxUint64),
	))

	properties.Property("AddDelegatorTx generator checks", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx, %w", err))
			}

			if err := signedTx.SyntacticVerify(ctx); err != nil {
				return err.Error()
			}

			addDelTx, ok := signedTx.Unsigned.(*txs.AddDelegatorTx)
			if !ok {
				return errNotAStakerTx.Error()
			}

			if nodeID != addDelTx.NodeID() {
				return errWrongNodeID.Error()
			}

			currentDel, err := NewCurrentStaker(signedTx.ID(), addDelTx, addDelTx.StartTime(), uint64(100))
			if err != nil {
				return err.Error()
			}

			if currentDel.EndTime.Before(currentDel.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					currentDel.StartTime, currentDel.EndTime, currentDel)
			}

			if currentDel.Weight > maxDelegatorWeight {
				return fmt.Sprintf("delegator weight %v above maximum %v, staker %v",
					currentDel.Weight, maxDelegatorWeight, currentDel)
			}

			pendingDel, err := NewPendingStaker(signedTx.ID(), addDelTx)
			if err != nil {
				return err.Error()
			}

			if pendingDel.EndTime.Before(pendingDel.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					pendingDel.StartTime, pendingDel.EndTime, pendingDel)
			}

			if pendingDel.Weight > maxDelegatorWeight {
				return fmt.Sprintf("delegator weight %v above maximum %v, staker %v",
					pendingDel.Weight, maxDelegatorWeight, pendingDel)
			}

			return ""
		},
		addDelegatorTxGenerator(ctx, &nodeID, maxDelegatorWeight),
	))

	properties.Property("addPermissionlessValidatorTx generator checks", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx, %w", err))
			}

			if err := signedTx.SyntacticVerify(ctx); err != nil {
				return err.Error()
			}

			addValTx, ok := signedTx.Unsigned.(*txs.AddPermissionlessValidatorTx)
			if !ok {
				return errNotAStakerTx.Error()
			}

			if nodeID != addValTx.NodeID() {
				return errWrongNodeID.Error()
			}

			if subnetID != addValTx.SubnetID() {
				return "subnet not duly set"
			}

			currentVal, err := NewCurrentStaker(signedTx.ID(), addValTx, addValTx.StartTime(), uint64(100))
			if err != nil {
				return err.Error()
			}

			if currentVal.EndTime.Before(currentVal.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					currentVal.StartTime, currentVal.EndTime, currentVal)
			}

			pendingVal, err := NewPendingStaker(signedTx.ID(), addValTx)
			if err != nil {
				return err.Error()
			}

			if pendingVal.EndTime.Before(pendingVal.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					pendingVal.StartTime, pendingVal.EndTime, pendingVal)
			}

			return ""
		},
		addPermissionlessValidatorTxGenerator(ctx, &subnetID, &nodeID, math.MaxUint64),
	))

	properties.Property("addPermissionlessDelegatorTx generator checks", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx, %w", err))
			}

			if err := signedTx.SyntacticVerify(ctx); err != nil {
				return err.Error()
			}

			addDelTx, ok := signedTx.Unsigned.(*txs.AddPermissionlessDelegatorTx)
			if !ok {
				return errNotAStakerTx.Error()
			}

			if nodeID != addDelTx.NodeID() {
				return errWrongNodeID.Error()
			}

			if subnetID != addDelTx.SubnetID() {
				return "subnet not duly set"
			}

			currentDel, err := NewCurrentStaker(signedTx.ID(), addDelTx, addDelTx.StartTime(), uint64(100))
			if err != nil {
				return err.Error()
			}

			if currentDel.EndTime.Before(currentDel.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					currentDel.StartTime, currentDel.EndTime, currentDel)
			}

			if currentDel.Weight > maxDelegatorWeight {
				return fmt.Sprintf("delegator weight %v above maximum %v, staker %v",
					currentDel.Weight, maxDelegatorWeight, currentDel)
			}

			pendingDel, err := NewPendingStaker(signedTx.ID(), addDelTx)
			if err != nil {
				return err.Error()
			}

			if pendingDel.EndTime.Before(pendingDel.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					pendingDel.StartTime, pendingDel.EndTime, pendingDel)
			}

			if pendingDel.Weight > maxDelegatorWeight {
				return fmt.Sprintf("delegator weight %v above maximum %v, staker %v",
					pendingDel.Weight, maxDelegatorWeight, pendingDel)
			}

			return ""
		},
		addPermissionlessDelegatorTxGenerator(ctx, &subnetID, &nodeID, maxDelegatorWeight),
	))

	properties.TestingRun(t)
}
