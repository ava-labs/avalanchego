// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"

	"github.com/ava-labs/avalanchego/snow"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilOutput                         = errors.New("nil output")
	errOutputUnspendable                 = errors.New("output is unspendable")
	errOutputUnoptimized                 = errors.New("output representation should be optimized")
	errAddrsNotSortedUnique              = errors.New("addresses not sorted and unique")
	errMarshal                           = errors.New("cannot marshal without ctx")
	_                       verify.State = &OutputOwners{}
)

// OutputOwners ...
type OutputOwners struct {
	Locktime  uint64        `serialize:"true" json:"locktime"`
	Threshold uint32        `serialize:"true" json:"threshold"`
	Addrs     []ids.ShortID `serialize:"true" json:"addresses"`
	// ctx is used in MarshalJSON to convert Addrs into human readable
	// format with ChainID and NetworkID. Unexported because we don't use
	// it outside this object.
	ctx *snow.Context `serialize:"false"`
}

// InitCtx assigns the OutputOwners.ctx object to given [ctx] object
// Must be called at least once for MarshalJSON to work successfully
func (out *OutputOwners) InitCtx(ctx *snow.Context) {
	out.ctx = ctx
}

// MarshalJSON marshals OutputOwners as JSON with human readable addresses.
// OutputOwners.InitCtx must be called before marshalling this or one of
// the parent objects to json. Uses the OutputOwners.ctx method to format
// the addresses. Returns errMarshal error if OutputOwners.ctx is not set.
func (out *OutputOwners) MarshalJSON() ([]byte, error) {
	// we need out.ctx to do this, if its absent, throw error
	if out.ctx == nil {
		return nil, errMarshal
	}

	result := make(map[string]interface{})
	result["locktime"] = out.Locktime
	result["threshold"] = out.Threshold

	addrsLen := len(out.Addrs)
	addresses := make([]string, addrsLen)
	for i, n := 0, addrsLen; i < n; i++ {
		// for each [addr] in [Addrs] we attempt to format it given
		// the [out.ctx] object
		addr := out.Addrs[i]
		fAddr, err := FormatAddress(out.ctx, addr)
		if err != nil {
			// we expect these addresses to be valid, return error
			// if they are not
			return nil, err
		}
		addresses[i] = fAddr
	}
	result["addresses"] = addresses

	return json.Marshal(result)
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
func (out *OutputOwners) AddressesSet() ids.ShortSet {
	set := ids.NewShortSet(len(out.Addrs))
	set.Add(out.Addrs...)
	return set
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

// Verify ...
func (out *OutputOwners) Verify() error {
	switch {
	case out == nil:
		return errNilOutput
	case out.Threshold > uint32(len(out.Addrs)):
		return errOutputUnspendable
	case out.Threshold == 0 && len(out.Addrs) > 0:
		return errOutputUnoptimized
	case !ids.IsSortedAndUniqueShortIDs(out.Addrs):
		return errAddrsNotSortedUnique
	default:
		return nil
	}
}

// VerifyState ...
func (out *OutputOwners) VerifyState() error { return out.Verify() }

// Sort ...
func (out *OutputOwners) Sort() { ids.SortShortIDs(out.Addrs) }

// FormatAddress formats a given [addr] into human readable format using
// [ChainID] and [NetworkID] from the provided [ctx].
func FormatAddress(ctx *snow.Context, addr ids.ShortID) (string, error) {
	chainIDAlias, err := ctx.BCLookup.PrimaryAlias(ctx.ChainID)
	if err != nil {
		return "", err
	}

	hrp := constants.GetHRP(ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
}
