// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errNilOutput            = errors.New("nil output")
	errOutputUnspendable    = errors.New("output is unspendable")
	errOutputUnoptimized    = errors.New("output representation should be optimized")
	errAddrsNotSortedUnique = errors.New("addresses not sorted and unique")
)

// OutputOwners ...
type OutputOwners struct {
	Locktime  uint64        `serialize:"true" json:"locktime"`
	Threshold uint32        `serialize:"true" json:"threshold"`
	Addrs     []ids.ShortID `serialize:"true" json:"addresses"`
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
	set := ids.ShortSet{}
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
		if !addr.Equals(otherAddr) {
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
