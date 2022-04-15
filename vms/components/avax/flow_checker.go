// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/math"
	"github.com/chain4travel/caminogo/utils/wrappers"
)

var errInsufficientFunds = errors.New("insufficient funds")

type FlowChecker struct {
	consumed, produced map[ids.ID]uint64
	errs               wrappers.Errs
}

func NewFlowChecker() *FlowChecker {
	return &FlowChecker{
		consumed: make(map[ids.ID]uint64),
		produced: make(map[ids.ID]uint64),
	}
}

func (fc *FlowChecker) Consume(assetID ids.ID, amount uint64) { fc.add(fc.consumed, assetID, amount) }

func (fc *FlowChecker) Produce(assetID ids.ID, amount uint64) { fc.add(fc.produced, assetID, amount) }

func (fc *FlowChecker) add(value map[ids.ID]uint64, assetID ids.ID, amount uint64) {
	var err error
	value[assetID], err = math.Add64(value[assetID], amount)
	fc.errs.Add(err)
}

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
