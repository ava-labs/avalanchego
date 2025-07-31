// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transfer

import (
	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	URIKey        = "uri"
	ChainIDKey    = "chain-id"
	MaxFeeKey     = "max-fee"
	AssetIDKey    = "asset-id"
	AmountKey     = "amount"
	ToKey         = "to"
	PrivateKeyKey = "private-key"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(URIKey, primary.LocalAPIURI, "API URI to use during issuance")
	flags.String(ChainIDKey, "", "Chain to issue the transaction on")
	flags.Uint64(MaxFeeKey, 0, "Maximum fee to spend")
	flags.String(AssetIDKey, "[chain-id]", "Asset to send")
	flags.Uint64(AmountKey, units.Schmeckle, "Amount to send")
	flags.String(ToKey, genesis.EWOQKey.Address().String(), "Destination address")
	flags.String(PrivateKeyKey, genesis.EWOQKeyFormattedStr, "Private key to sign the transaction")
}

type Config struct {
	URI        string
	ChainID    ids.ID
	MaxFee     uint64
	AssetID    ids.ID
	Amount     uint64
	To         ids.ShortID
	PrivateKey *secp256k1.PrivateKey
}

func ParseFlags(flags *pflag.FlagSet, args []string) (*Config, error) {
	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	uri, err := flags.GetString(URIKey)
	if err != nil {
		return nil, err
	}

	chainIDStr, err := flags.GetString(ChainIDKey)
	if err != nil {
		return nil, err
	}

	chainID, err := ids.FromString(chainIDStr)
	if err != nil {
		return nil, err
	}

	maxFee, err := flags.GetUint64(MaxFeeKey)
	if err != nil {
		return nil, err
	}

	assetID := chainID
	if flags.Changed(AssetIDKey) {
		assetIDStr, err := flags.GetString(AssetIDKey)
		if err != nil {
			return nil, err
		}

		assetID, err = ids.FromString(assetIDStr)
		if err != nil {
			return nil, err
		}
	}

	amount, err := flags.GetUint64(AmountKey)
	if err != nil {
		return nil, err
	}

	toStr, err := flags.GetString(ToKey)
	if err != nil {
		return nil, err
	}

	to, err := ids.ShortFromString(toStr)
	if err != nil {
		return nil, err
	}

	skStr, err := flags.GetString(PrivateKeyKey)
	if err != nil {
		return nil, err
	}

	var sk secp256k1.PrivateKey
	err = sk.UnmarshalText([]byte(`"` + skStr + `"`))
	if err != nil {
		return nil, err
	}

	return &Config{
		URI:        uri,
		ChainID:    chainID,
		MaxFee:     maxFee,
		AssetID:    assetID,
		Amount:     amount,
		To:         to,
		PrivateKey: &sk,
	}, nil
}
