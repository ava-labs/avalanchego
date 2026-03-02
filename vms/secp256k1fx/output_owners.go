// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	ErrNilOutput            = errors.New("nil output")
	ErrOutputUnspendable    = errors.New("output is unspendable")
	ErrOutputUnoptimized    = errors.New("output representation should be optimized")
	ErrAddrsNotSortedUnique = errors.New("addresses not sorted and unique")
)

type OutputOwners struct {
	verify.IsNotState `json:"-"`

	Locktime  uint64        `serialize:"true" json:"locktime"`
	Threshold uint32        `serialize:"true" json:"threshold"`
	Addrs     []ids.ShortID `serialize:"true" json:"addresses"`

	// ctx is used in MarshalJSON to convert Addrs into human readable
	// format with ChainID and NetworkID. Unexported because we don't use
	// it outside this object.
	ctx *snow.Context
}

// InitCtx allows addresses to be formatted into their human readable format
// during json marshalling.
func (out *OutputOwners) InitCtx(ctx *snow.Context) {
	out.ctx = ctx
}

// MarshalJSON marshals OutputOwners as JSON with human readable addresses.
// OutputOwners.InitCtx must be called before marshalling this or one of
// the parent objects to json. Uses the OutputOwners.ctx method to format
// the addresses. Returns errMarshal error if OutputOwners.ctx is not set.
func (out *OutputOwners) MarshalJSON() ([]byte, error) {
	result, err := out.Fields()
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

// Fields returns JSON keys in a map that can be used with marshal JSON
// to serialize OutputOwners struct
func (out *OutputOwners) Fields() (map[string]interface{}, error) {
	addresses := make([]string, len(out.Addrs))
	for i, addr := range out.Addrs {
		// for each [addr] in [Addrs] we attempt to format it given
		// the [out.ctx] object
		fAddr, err := formatAddress(out.ctx, addr)
		if err != nil {
			// we expect these addresses to be valid, return error
			// if they are not
			return nil, err
		}
		addresses[i] = fAddr
	}
	result := map[string]interface{}{
		"locktime":  out.Locktime,
		"threshold": out.Threshold,
		"addresses": addresses,
	}

	return result, nil
}

// Addresses returns the addresses that manage this output
func (out *OutputOwners) Addresses() [][]byte {
	addrs := make([][]byte, len(out.Addrs))
	for i, addr := range out.Addrs {
		addrs[i] = addr.Bytes()
	}
	return addrs
}

// AddressesSet returns addresses as a set
func (out *OutputOwners) AddressesSet() set.Set[ids.ShortID] {
	return set.Of(out.Addrs...)
}

// Equals returns true if the provided owners create the same condition
func (out *OutputOwners) Equals(other *OutputOwners) bool {
	if out == other {
		return true
	}
	if out == nil || other == nil || out.Locktime != other.Locktime || out.Threshold != other.Threshold || len(out.Addrs) != len(other.Addrs) {
		return false
	}
	for i, addr := range out.Addrs {
		otherAddr := other.Addrs[i]
		if addr != otherAddr {
			return false
		}
	}
	return true
}

func (out *OutputOwners) Verify() error {
	switch {
	case out == nil:
		return ErrNilOutput
	case out.Threshold > uint32(len(out.Addrs)):
		return ErrOutputUnspendable
	case out.Threshold == 0 && len(out.Addrs) > 0:
		return ErrOutputUnoptimized
	case !utils.IsSortedAndUnique(out.Addrs):
		return ErrAddrsNotSortedUnique
	default:
		return nil
	}
}

func (out *OutputOwners) Sort() {
	utils.Sort(out.Addrs)
}

// formatAddress formats a given [addr] into human readable format using
// [ChainID] and [NetworkID] if a non-nil [ctx] is provided. If [ctx] is not
// provided, the address will be returned in cb58 format.
func formatAddress(ctx *snow.Context, addr ids.ShortID) (string, error) {
	if ctx == nil {
		return addr.String(), nil
	}

	chainIDAlias, err := ctx.BCLookup.PrimaryAlias(ctx.ChainID)
	if err != nil {
		return "", err
	}

	hrp := constants.GetHRP(ctx.NetworkID)
	return address.Format(chainIDAlias, hrp, addr.Bytes())
}
