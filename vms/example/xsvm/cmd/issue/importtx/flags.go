// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package importtx

import (
	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	URIKey                = "uri"
	SourceURIsKey         = "source-uris"
	SourceChainIDKey      = "source-chain-id"
	DestinationChainIDKey = "destination-chain-id"
	TxIDKey               = "tx-id"
	MaxFeeKey             = "max-fee"
	PrivateKeyKey         = "private-key"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(URIKey, primary.LocalAPIURI, "API URI to use during issuance")
	flags.StringSlice(SourceURIsKey, []string{primary.LocalAPIURI}, "API URIs to use during the fetching of signatures")
	flags.String(SourceChainIDKey, "", "Chain the export transaction was issued on")
	flags.String(DestinationChainIDKey, "", "Chain to send the asset to")
	flags.String(TxIDKey, "", "ID of the export transaction")
	flags.Uint64(MaxFeeKey, 0, "Maximum fee to spend")
	flags.String(PrivateKeyKey, genesis.EWOQKeyFormattedStr, "Private key to sign the transaction")
}

type Config struct {
	URI                string
	SourceURIs         []string
	SourceChainID      string
	DestinationChainID string
	TxID               ids.ID
	MaxFee             uint64
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

	sourceURIs, err := flags.GetStringSlice(SourceURIsKey)
	if err != nil {
		return nil, err
	}

	sourceChainID, err := flags.GetString(SourceChainIDKey)
	if err != nil {
		return nil, err
	}

	destinationChainID, err := flags.GetString(DestinationChainIDKey)
	if err != nil {
		return nil, err
	}

	txIDStr, err := flags.GetString(TxIDKey)
	if err != nil {
		return nil, err
	}

	txID, err := ids.FromString(txIDStr)
	if err != nil {
		return nil, err
	}

	maxFee, err := flags.GetUint64(MaxFeeKey)
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
		SourceURIs:         sourceURIs,
		SourceChainID:      sourceChainID,
		DestinationChainID: destinationChainID,
		TxID:               txID,
		MaxFee:             maxFee,
		PrivateKey:         &sk,
	}, nil
}
