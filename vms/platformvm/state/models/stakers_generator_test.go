// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	blst "github.com/supranational/blst/bindings/go"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

var StakerGenerator = genStakerTimeData.FlatMap(
	func(v interface{}) gopter.Gen {
		macro := v.(stakerTimeData)

		return gen.Struct(reflect.TypeOf(state.Staker{}), map[string]gopter.Gen{
			"TxID":            genID,
			"NodeID":          genNodeID,
			"PublicKey":       genBlsKey,
			"SubnetID":        genID,
			"Weight":          gen.UInt64(),
			"StartTime":       gen.Const(macro.StartTime),
			"EndTime":         gen.Const(macro.EndTime),
			"PotentialReward": gen.UInt64(),
			"NextTime":        gen.Const(macro.NextTime),
			"Priority":        gen.Const(macro.Priority),
		})
	},
	reflect.TypeOf(stakerTimeData{}),
)

func TestGeneratedStakersValidity(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("EndTime never before StartTime", prop.ForAll(
		func(s state.Staker) string {
			if s.EndTime.Before(s.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					s.StartTime, s.EndTime, s)
			}
			return ""
		},
		StakerGenerator,
	))

	properties.Property("NextTime coherent with priority", prop.ForAll(
		func(s state.Staker) string {
			switch p := s.Priority; p {
			case txs.PrimaryNetworkDelegatorApricotPendingPriority,
				txs.PrimaryNetworkDelegatorBanffPendingPriority,
				txs.SubnetPermissionlessDelegatorPendingPriority,
				txs.PrimaryNetworkValidatorPendingPriority,
				txs.SubnetPermissionlessValidatorPendingPriority,
				txs.SubnetPermissionedValidatorPendingPriority:
				if !s.NextTime.Equal(s.StartTime) {
					return fmt.Sprintf("pending staker has nextTime %v different from startTime %v, staker %v",
						s.NextTime, s.StartTime, s)
				}
				return ""

			case txs.PrimaryNetworkDelegatorCurrentPriority,
				txs.SubnetPermissionlessDelegatorCurrentPriority,
				txs.PrimaryNetworkValidatorCurrentPriority,
				txs.SubnetPermissionlessValidatorCurrentPriority,
				txs.SubnetPermissionedValidatorCurrentPriority:
				if !s.NextTime.Equal(s.EndTime) {
					return fmt.Sprintf("current staker has nextTime %v different from endTime %v, staker %v",
						s.NextTime, s.EndTime, s)
				}
				return ""

			default:
				return fmt.Sprintf("priority %v unhandled in test", p)
			}
		},
		StakerGenerator,
	))

	properties.TestingRun(t)
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

var genStakerTimeData = genStakerMicroData.FlatMap(
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

// stakerMicroData holds seed attributes to generate stakerMacroData
type stakerMicroData struct {
	StartTime time.Time
	Duration  int64
	Priority  txs.Priority
}

// genStakerMicroData is the helper to generate stakerMicroData
var genStakerMicroData = gen.Struct(reflect.TypeOf(&stakerMicroData{}), map[string]gopter.Gen{
	"StartTime": gen.Time(),
	"Duration":  gen.Int64Range(1, 365*24),
	"Priority": gen.OneConstOf(
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
	),
})

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
