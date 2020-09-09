// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/math"
	"github.com/ava-labs/avalanche-go/utils/wrappers"
)

var (
	errInsufficientFunds = errors.New("insufficient funds")
)

// FlowChecker ...
type FlowChecker struct {
	consumed, produced map[[32]byte]uint64
	errs               wrappers.Errs
}

// NewFlowChecker ...
func NewFlowChecker() *FlowChecker {
	return &FlowChecker{
		consumed: make(map[[32]byte]uint64),
		produced: make(map[[32]byte]uint64),
	}
}

// Consume ...
func (fc *FlowChecker) Consume(assetID ids.ID, amount uint64) { fc.add(fc.consumed, assetID, amount) }

// Produce ...
func (fc *FlowChecker) Produce(assetID ids.ID, amount uint64) { fc.add(fc.produced, assetID, amount) }

func (fc *FlowChecker) add(value map[[32]byte]uint64, assetID ids.ID, amount uint64) {
	var err error
	assetIDKey := assetID.Key()
	value[assetIDKey], err = math.Add64(value[assetIDKey], amount)
	fc.errs.Add(err)
}

// Verify ...
func (fc *FlowChecker) Verify() error {
	if !fc.errs.Errored() {
		for assetID, producedAssetAmount := range fc.produced {
			consumedAssetAmount := fc.consumed[assetID]
			if producedAssetAmount > consumedAssetAmount {
				fc.errs.Add(errInsufficientFunds)
				break
			}
		}
	}
	return fc.errs.Err
}
