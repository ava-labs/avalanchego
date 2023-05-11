// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"reflect"
	"time"

	blst "github.com/supranational/blst/bindings/go"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
)

type generatorPriorityType uint8

const (
	anyPriority generatorPriorityType = iota
	currentValidator
	currentDelegator
	pendingValidator
	pendingDelegator
)

// stakerGenerator helps creating random yet reproducible Staker objects,
// which can be used in our property tests. stakerGenerator takes care of
// enforcing some Staker invariants on each and every random sample.
// TestGeneratedStakersValidity documents and verifies the enforced invariants.
func stakerGenerator(
	prio generatorPriorityType,
	subnet *ids.ID,
	nodeID *ids.NodeID,
	maxWeight uint64, // helps avoiding overflows in delegator tests,
) gopter.Gen {
	return genStakerTimeData(prio).FlatMap(
		func(v interface{}) gopter.Gen {
			macro := v.(stakerTimeData)

			genStakerSubnetID := genID
			genStakerNodeID := genNodeID
			if subnet != nil {
				genStakerSubnetID = gen.Const(*subnet)
			}
			if nodeID != nil {
				genStakerNodeID = gen.Const(*nodeID)
			}

			return gen.Struct(reflect.TypeOf(Staker{}), map[string]gopter.Gen{
				"TxID":            genID,
				"NodeID":          genStakerNodeID,
				"PublicKey":       genBlsKey,
				"SubnetID":        genStakerSubnetID,
				"Weight":          gen.UInt64Range(0, maxWeight),
				"StartTime":       gen.Const(macro.StartTime),
				"EndTime":         gen.Const(macro.EndTime),
				"PotentialReward": gen.UInt64(),
				"NextTime":        gen.Const(macro.NextTime),
				"Priority":        gen.Const(macro.Priority),
			})
		},
		reflect.TypeOf(stakerTimeData{}),
	)
}

// stakerTimeData holds Staker's time related data in order to generate them
// while fullfilling the following constrains:
// 1. EndTime >= StartTime
// 2. NextTime == EndTime for current priorities
// 3. NextTime == StartTime for pending priorities
type stakerTimeData struct {
	StartTime time.Time
	EndTime   time.Time
	Priority  txs.Priority
	NextTime  time.Time
}

func genStakerTimeData(prio generatorPriorityType) gopter.Gen {
	return genStakerMicroData(prio).FlatMap(
		func(v interface{}) gopter.Gen {
			micro := v.(stakerMicroData)

			var (
				startTime = micro.StartTime
				endTime   = micro.StartTime.Add(time.Duration(micro.Duration * int64(time.Hour)))
				priority  = micro.Priority
			)

			startTimeGen := gen.Const(startTime)
			endTimeGen := gen.Const(endTime)
			priorityGen := gen.Const(priority)
			var nextTimeGen gopter.Gen
			if priority == txs.SubnetPermissionedValidatorCurrentPriority ||
				priority == txs.SubnetPermissionlessDelegatorCurrentPriority ||
				priority == txs.SubnetPermissionlessValidatorCurrentPriority ||
				priority == txs.PrimaryNetworkDelegatorCurrentPriority ||
				priority == txs.PrimaryNetworkValidatorCurrentPriority {
				nextTimeGen = gen.Const(endTime)
			} else {
				nextTimeGen = gen.Const(startTime)
			}

			return gen.Struct(reflect.TypeOf(stakerTimeData{}), map[string]gopter.Gen{
				"StartTime": startTimeGen,
				"EndTime":   endTimeGen,
				"Priority":  priorityGen,
				"NextTime":  nextTimeGen,
			})
		},
		reflect.TypeOf(stakerMicroData{}),
	)
}

// stakerMicroData holds seed attributes to generate stakerMacroData
type stakerMicroData struct {
	StartTime time.Time
	Duration  int64
	Priority  txs.Priority
}

// genStakerMicroData is the helper to generate stakerMicroData
func genStakerMicroData(prio generatorPriorityType) gopter.Gen {
	return gen.Struct(reflect.TypeOf(&stakerMicroData{}), map[string]gopter.Gen{
		"StartTime": gen.Time(),
		"Duration":  gen.Int64Range(1, 365*24),
		"Priority":  genPriority(prio),
	})
}

func genPriority(p generatorPriorityType) gopter.Gen {
	switch p {
	case anyPriority:
		return gen.OneConstOf(
			txs.PrimaryNetworkDelegatorApricotPendingPriority,
			txs.PrimaryNetworkValidatorPendingPriority,
			txs.PrimaryNetworkDelegatorBanffPendingPriority,
			txs.SubnetPermissionlessValidatorPendingPriority,
			txs.SubnetPermissionlessDelegatorPendingPriority,
			txs.SubnetPermissionedValidatorPendingPriority,
			txs.SubnetPermissionedValidatorCurrentPriority,
			txs.SubnetPermissionlessDelegatorCurrentPriority,
			txs.SubnetPermissionlessValidatorCurrentPriority,
			txs.PrimaryNetworkDelegatorCurrentPriority,
			txs.PrimaryNetworkValidatorCurrentPriority,
		)
	case currentValidator:
		return gen.OneConstOf(
			txs.SubnetPermissionedValidatorCurrentPriority,
			txs.SubnetPermissionlessValidatorCurrentPriority,
			txs.PrimaryNetworkValidatorCurrentPriority,
		)
	case currentDelegator:
		return gen.OneConstOf(
			txs.SubnetPermissionlessDelegatorCurrentPriority,
			txs.PrimaryNetworkDelegatorCurrentPriority,
		)
	case pendingValidator:
		return gen.OneConstOf(
			txs.PrimaryNetworkValidatorPendingPriority,
			txs.SubnetPermissionlessValidatorPendingPriority,
			txs.SubnetPermissionedValidatorPendingPriority,
		)
	case pendingDelegator:
		return gen.OneConstOf(
			txs.PrimaryNetworkDelegatorApricotPendingPriority,
			txs.PrimaryNetworkDelegatorBanffPendingPriority,
			txs.SubnetPermissionlessDelegatorPendingPriority,
		)
	default:
		panic("unhandled priority type")
	}
}

var genBlsKey = gen.SliceOfN(lengthID, gen.UInt8()).FlatMap(
	func(v interface{}) gopter.Gen {
		byteSlice := v.([]byte)
		sk := blst.KeyGen(byteSlice)
		pk := bls.PublicFromSecretKey(sk)
		return gen.Const(pk)
	},
	reflect.TypeOf([]byte{}),
)

const (
	lengthID     = 32
	lengthNodeID = 20
)

// genID is the helper generator for ids.ID objects
var genID = gen.SliceOfN(lengthID, gen.UInt8()).FlatMap(
	func(v interface{}) gopter.Gen {
		byteSlice := v.([]byte)
		var byteArray [lengthID]byte
		copy(byteArray[:], byteSlice)
		return gen.Const(ids.ID(byteArray))
	},
	reflect.TypeOf([]byte{}),
)

// genID is the helper generator for ids.NodeID objects
var genNodeID = gen.SliceOfN(lengthNodeID, gen.UInt8()).FlatMap(
	func(v interface{}) gopter.Gen {
		byteSlice := v.([]byte)
		var byteArray [lengthNodeID]byte
		copy(byteArray[:], byteSlice)
		return gen.Const(ids.NodeID(byteArray))
	},
	reflect.TypeOf([]byte{}),
)
