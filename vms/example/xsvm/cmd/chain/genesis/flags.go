// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"math"
	"time"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"

	xsgenesis "github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
)

const (
	TimeKey     = "time"
	AddressKey  = "address"
	BalanceKey  = "balance"
	EncodingKey = "encoding"

	binaryEncoding = "binary"
	hexEncoding    = "hex"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.Int64(TimeKey, time.Now().Unix(), "Unix timestamp to include in the genesis")
	flags.String(AddressKey, genesis.EWOQKey.Address().String(), "Address to fund in the genesis")
	flags.Uint64(BalanceKey, math.MaxUint64, "Amount to provide the funded address in the genesis")
	flags.String(EncodingKey, hexEncoding, fmt.Sprintf("Encoding to use for the genesis. Available values: %s or %s", hexEncoding, binaryEncoding))
}

type Config struct {
	Genesis  *xsgenesis.Genesis
	Encoding string
}

func ParseFlags(flags *pflag.FlagSet, args []string) (*Config, error) {
	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	timestamp, err := flags.GetInt64(TimeKey)
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

	encoding, err := flags.GetString(EncodingKey)
	if err != nil {
		return nil, err
	}

	return &Config{
		Genesis: &xsgenesis.Genesis{
			Timestamp: timestamp,
			Allocations: []xsgenesis.Allocation{
				{
					Address: addr,
					Balance: balance,
				},
			},
		},
		Encoding: encoding,
	}, nil
}
