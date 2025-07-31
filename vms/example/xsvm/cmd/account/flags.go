// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package account

import (
	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	URIKey     = "uri"
	ChainIDKey = "chain-id"
	AddressKey = "address"
	AssetIDKey = "asset-id"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(URIKey, primary.LocalAPIURI, "API URI to use to fetch the account state")
	flags.String(ChainIDKey, "", "Chain to fetch the account state on")
	flags.String(AddressKey, genesis.EWOQKey.Address().String(), "Address of the account to fetch")
	flags.String(AssetIDKey, "[chain-id]", "Asset balance to fetch")
}

type Config struct {
	URI     string
	ChainID string
	Address ids.ShortID
	AssetID ids.ID
}

func ParseFlags(flags *pflag.FlagSet, args []string) (*Config, error) {
	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	uri, err := flags.GetString(URIKey)
	if err != nil {
		return nil, err
	}

	chainID, err := flags.GetString(ChainIDKey)
	if err != nil {
		return nil, err
	}

	addrStr, err := flags.GetString(AddressKey)
	if err != nil {
		return nil, err
	}

	addr, err := ids.ShortFromString(addrStr)
	if err != nil {
		return nil, err
	}

	assetIDStr := chainID
	if flags.Changed(AssetIDKey) {
		assetIDStr, err = flags.GetString(AssetIDKey)
		if err != nil {
			return nil, err
		}
	}

	assetID, err := ids.FromString(assetIDStr)
	if err != nil {
		return nil, err
	}

	return &Config{
		URI:     uri,
		ChainID: chainID,
		Address: addr,
		AssetID: assetID,
	}, nil
}
