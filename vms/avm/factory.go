// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Factory = &Factory{}

type Factory struct {
	TxFee            uint64
	CreateAssetTxFee uint64

	// Time of the Blueberry network upgrade
	BlueberryTime time.Time
}

func (f *Factory) IsBlueberryActivated(timestamp time.Time) bool {
	return !timestamp.Before(f.BlueberryTime)
}

func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{Factory: *f}, nil
}
