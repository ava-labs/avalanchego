// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package create

import (
	"math"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	URIKey        = "uri"
	SubnetIDKey   = "subnet-id"
	AddressKey    = "address"
	BalanceKey    = "balance"
	NameKey       = "name"
	PrivateKeyKey = "private-key"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(URIKey, primary.LocalAPIURI, "API URI to use to issue the chain creation transaction")
	flags.String(SubnetIDKey, "", "Subnet to create the chain under")
	flags.String(AddressKey, genesis.EWOQKey.Address().String(), "Address to fund in the genesis")
	flags.Uint64(BalanceKey, math.MaxUint64, "Amount to provide the funded address in the genesis")
	flags.String(NameKey, "xs", "Name of the chain to create")
	flags.String(PrivateKeyKey, genesis.EWOQKeyFormattedStr, "Private key to use when creating the new chain")
}

type Config struct {
	URI        string
	SubnetID   ids.ID
	Address    ids.ShortID
	Balance    uint64
	Name       string
	PrivateKey *secp256k1.PrivateKey
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

	subnetIDStr, err := flags.GetString(SubnetIDKey)
	if err != nil {
		return nil, err
	}

	subnetID, err := ids.FromString(subnetIDStr)
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

	balance, err := flags.GetUint64(BalanceKey)
	if err != nil {
		return nil, err
	}

	name, err := flags.GetString(NameKey)
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
		SubnetID:   subnetID,
		Address:    addr,
		Balance:    balance,
		Name:       name,
		PrivateKey: &sk,
	}, nil
}
