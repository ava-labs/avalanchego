// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilAssetID   = errors.New("nil asset ID is not valid")
	errEmptyAssetID = errors.New("empty asset ID is not valid")

	_ verify.Verifiable = (*Asset)(nil)
)

type Asset struct {
	ID ids.ID `serialize:"true" json:"assetID"`
}

// AssetID returns the ID of the contained asset
func (asset *Asset) AssetID() ids.ID {
	return asset.ID
}

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
