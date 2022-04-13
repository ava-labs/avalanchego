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
	"github.com/chain4travel/caminogo/vms/components/verify"
)

var (
	errNilAssetID   = errors.New("nil asset ID is not valid")
	errEmptyAssetID = errors.New("empty asset ID is not valid")

	_ verify.Verifiable = &Asset{}
)

type Asset struct {
	ID ids.ID `serialize:"true" json:"assetID"`
}

// AssetID returns the ID of the contained asset
func (asset *Asset) AssetID() ids.ID { return asset.ID }

func (asset *Asset) Verify() error {
	switch {
	case asset == nil:
		return errNilAssetID
	case asset.ID == ids.Empty:
		return errEmptyAssetID
	default:
		return nil
	}
}
