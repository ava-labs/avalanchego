// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package export

import (
	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	URIKey                = "uri"
	SourceChainIDKey      = "source-chain-id"
	DestinationChainIDKey = "destination-chain-id"
	MaxFeeKey             = "max-fee"
	IsReturnKey           = "is-return"
	AmountKey             = "amount"
	ToKey                 = "to"
	PrivateKeyKey         = "private-key"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(URIKey, primary.LocalAPIURI, "API URI to use during issuance")
	flags.String(SourceChainIDKey, "", "Chain to issue the transaction on")
	flags.String(DestinationChainIDKey, "", "Chain to send the asset to")
	flags.Uint64(MaxFeeKey, 0, "Maximum fee to spend")
	flags.Bool(IsReturnKey, false, "Mark this transaction as returning funds")
	flags.Uint64(AmountKey, units.Schmeckle, "Amount to send")
	flags.String(ToKey, genesis.EWOQKey.Address().String(), "Destination address")
	flags.String(PrivateKeyKey, genesis.EWOQKeyFormattedStr, "Private key to sign the transaction")
}

type Config struct {
	URI                string
	SourceChainID      ids.ID
	DestinationChainID ids.ID
	MaxFee             uint64
	IsReturn           bool
	Amount             uint64
	To                 ids.ShortID
	PrivateKey         *secp256k1.PrivateKey
}

func ParseFlags(flags *pflag.FlagSet, args []string) (*Config, error) {
	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	uri, err := flags.GetString(URIKey)
	if err != nil {
		return nil, err
	}

	sourceChainIDStr, err := flags.GetString(SourceChainIDKey)
	if err != nil {
		return nil, err
	}

	sourceChainID, err := ids.FromString(sourceChainIDStr)
	if err != nil {
		return nil, err
	}

	destinationChainIDStr, err := flags.GetString(DestinationChainIDKey)
	if err != nil {
		return nil, err
	}

	destinationChainID, err := ids.FromString(destinationChainIDStr)
	if err != nil {
		return nil, err
	}

	maxFee, err := flags.GetUint64(MaxFeeKey)
	if err != nil {
		return nil, err
	}

	isReturn, err := flags.GetBool(IsReturnKey)
	if err != nil {
		return nil, err
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
		URI:                uri,
		SourceChainID:      sourceChainID,
		DestinationChainID: destinationChainID,
		MaxFee:             maxFee,
		IsReturn:           isReturn,
		Amount:             amount,
		To:                 to,
		PrivateKey:         &sk,
	}, nil
}
