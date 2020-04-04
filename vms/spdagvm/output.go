// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
)

// Output describes what functions every output must implement
type Output interface {
	formatting.PrefixedStringer

	Unlock(Input, uint64) error
	Verify() error
}

// OutputPayment represents an output that transfers value
// OutputPayment implements Output
type OutputPayment struct {
	// The amount of this output
	amount uint64

	// The earliest time at which this output may be spent
	// Measured in Unix time
	locktime uint64

	// The number of signatures required to spend this output
	threshold uint32

	// The addresses that can produce signatures to spend this output
	addresses []ids.ShortID
}

// Amount of value this output creates
func (op *OutputPayment) Amount() uint64 { return op.amount }

// Locktime is the time that this output should be able to be spent
func (op *OutputPayment) Locktime() uint64 { return op.locktime }

// Threshold is the number of signatures this output will require to be spent
func (op *OutputPayment) Threshold() uint32 { return op.threshold }

// Addresses are the representations of keys that can produce signatures to
// spend this output
func (op *OutputPayment) Addresses() []ids.ShortID { return op.addresses }

// Unlock returns true if the input has the correct signatures to spend this
// output
func (op *OutputPayment) Unlock(in Input, time uint64) error {
	if op.locktime > time {
		return errTimelocked
	}
	switch i := in.(type) {
	case *InputPayment:
		switch {
		case op.amount != i.amount:
			return errInvalidAmount
		case !checkRawAddresses(op.threshold, op.addresses, i.sigs):
			return errSpendFailed
		}
	default:
		return errTypeMismatch
	}
	return nil
}

// Verify that this output is syntactically correct
func (op *OutputPayment) Verify() error {
	switch {
	case op == nil:
		return errNilOutput
	case op.amount == 0:
		return errOutputHasNoValue
	case op.threshold > uint32(len(op.addresses)):
		return errOutputUnspendable
	case op.threshold == 0 && len(op.addresses) > 0:
		return errOutputUnoptimized
	case !ids.IsSortedAndUniqueShortIDs(op.addresses):
		return errAddrsNotSortedUnique
	default:
		return nil
	}
}

// PrefixedString converts this input to a string representation with a prefix
// for each newline
func (op *OutputPayment) PrefixedString(prefix string) string {
	s := strings.Builder{}

	s.WriteString(fmt.Sprintf("OutputPayment(\n"+
		"%s    Amount    = %d\n"+
		"%s    Locktime  = %d\n"+
		"%s    Threshold = %d\n"+
		"%s    NumAddrs  = %d\n",
		prefix, op.amount,
		prefix, op.locktime,
		prefix, op.threshold,
		prefix, len(op.addresses)))

	addrFormat := fmt.Sprintf("%%s    Addrs[%s]: %%s\n",
		formatting.IntFormat(len(op.addresses)-1))
	for i, addr := range op.addresses {
		s.WriteString(fmt.Sprintf(addrFormat,
			prefix, i, addr,
		))
	}

	s.WriteString(fmt.Sprintf("%s)", prefix))

	return s.String()
}

func (op *OutputPayment) String() string { return op.PrefixedString("") }

// OutputTakeOrLeave is a take-or-leave transaction. It implements Output.
// After time [locktime1], it can be spent using [threshold1] signatures, where each
// signature is from [addresses1]
// After time [locktime2], it can also be spent using [threshold2] signatures, where each
// signature is from [addresses2]
type OutputTakeOrLeave struct {
	// The amount of this output
	amount uint64

	// The time (Unix time) after which this output may be spent
	// using [threshold1] signatures from [addresses1]
	locktime1 uint64

	// The time (Unix time) after which this output may be spent
	// using [threshold1] signatures from [addresses1]
	// Must be greater than [locktime1]
	locktime2 uint64

	// The number of signatures from [addresses1] required to spend
	// this output
	threshold1 uint32

	// The number of signatures from [addresses2] required to spend
	// this output
	threshold2 uint32

	// The addresses that may spend this output after [locktime1]
	addresses1 []ids.ShortID

	// The addresses that may spend this output after [locktime2]
	addresses2 []ids.ShortID
}

// Amount returns the value this output produces
func (otol *OutputTakeOrLeave) Amount() uint64 { return otol.amount }

// Locktime1 returns the time after which the first set of addresses
// may spend this output are unlocked
func (otol *OutputTakeOrLeave) Locktime1() uint64 { return otol.locktime1 }

// Threshold1 returns the number of signatures the first set of addresses need to
// produce to be able to spend this output
func (otol *OutputTakeOrLeave) Threshold1() uint32 { return otol.threshold1 }

// Addresses1 are the addresses controlled by keys that can produce signatures to
// spend this output
func (otol *OutputTakeOrLeave) Addresses1() []ids.ShortID { return otol.addresses1 }

// Locktime2 returns when the second set of addresses are unlocked
func (otol *OutputTakeOrLeave) Locktime2() uint64 { return otol.locktime2 }

// Threshold2 returns the number of signatures the second set of addresses
// need to produce to be able to spend this output
func (otol *OutputTakeOrLeave) Threshold2() uint32 { return otol.threshold2 }

// Addresses2 are the addresses controlled by keys that can produce signatures to
// spend this output
func (otol *OutputTakeOrLeave) Addresses2() []ids.ShortID { return otol.addresses2 }

// Unlock returns true if the input has the correct signatures to spend this
// output at time [time]
func (otol *OutputTakeOrLeave) Unlock(in Input, time uint64) error {
	switch i := in.(type) {
	case *InputPayment:
		switch {
		case otol.amount != i.amount:
			return errInvalidAmount
		case otol.locktime2 > time:
			return errTimelocked
		case (otol.locktime1 > time ||
			!checkRawAddresses(otol.threshold1, otol.addresses1, i.sigs)) &&
			!checkRawAddresses(otol.threshold2, otol.addresses2, i.sigs):
			return errSpendFailed
		}
	default:
		return errTypeMismatch
	}
	return nil
}

// Verify that this output is syntactically correct
func (otol *OutputTakeOrLeave) Verify() error {
	switch {
	case otol == nil:
		return errNilOutput
	case otol.amount == 0:
		return errOutputHasNoValue
	case otol.threshold1 > uint32(len(otol.addresses1)) ||
		otol.threshold2 > uint32(len(otol.addresses2)):
		return errOutputUnspendable
	case (otol.threshold1 == 0 && len(otol.addresses1) > 0) ||
		(otol.threshold2 == 0 && len(otol.addresses2) > 0):
		return errOutputUnoptimized
	case otol.locktime1 >= otol.locktime2:
		return errTimesNotSortedUnique
	case !ids.IsSortedAndUniqueShortIDs(otol.addresses1) ||
		!ids.IsSortedAndUniqueShortIDs(otol.addresses2):
		return errAddrsNotSortedUnique
	default:
		return nil
	}
}

// PrefixedString converts this input to a string representation with a prefix
// for each newline
func (otol *OutputTakeOrLeave) PrefixedString(prefix string) string {
	s := strings.Builder{}

	s.WriteString(fmt.Sprintf("OutputTakeOrLeave(\n"+
		"%s    Amount        = %d\n"+
		"%s    Locktime      = %d\n"+
		"%s    Threshold     = %d\n"+
		"%s    NumAddrs      = %d\n",
		prefix, otol.amount,
		prefix, otol.locktime1,
		prefix, otol.threshold1,
		prefix, len(otol.addresses1)))

	addrFormat := fmt.Sprintf("%%s    Addrs[%s]: %%s\n",
		formatting.IntFormat(len(otol.addresses1)-1))
	for i, addr := range otol.addresses1 {
		s.WriteString(fmt.Sprintf(addrFormat,
			prefix, i, addr,
		))
	}

	s.WriteString(fmt.Sprintf("%s    FallLocktime  = %d\n"+
		"%s    FallThreshold = %d\n"+
		"%s    FallNumAddrs  = %d\n",
		prefix, otol.locktime2,
		prefix, otol.threshold2,
		prefix, len(otol.addresses2)))

	fallAddrFormat := fmt.Sprintf("%%s    FallAddrs[%s]: %%s\n",
		formatting.IntFormat(len(otol.addresses2)-1))
	for i, addr := range otol.addresses2 {
		s.WriteString(fmt.Sprintf(fallAddrFormat,
			prefix, i, addr,
		))
	}

	s.WriteString(fmt.Sprintf("%s)", prefix))

	return s.String()
}

func (otol *OutputTakeOrLeave) String() string { return otol.PrefixedString("") }

// checkRange returns true if [index] is in the range [l, u).
func checkRange(index, l, u int) bool {
	return l <= index && index < u
}

// checkRawAddresses checks that the signatures match with the addresses and
// that the threshold is the expected value.
func checkRawAddresses(threshold uint32, addrs []ids.ShortID, sigs []*Sig) bool {
	if !crypto.EnableCrypto {
		return true
	}
	if uint32(len(sigs)) != threshold {
		return false
	}
	for _, sig := range sigs {
		i := int(sig.index)
		if !checkRange(i, 0, len(addrs)) || !bytes.Equal(addrs[i].Bytes(), hashing.PubkeyBytesToAddress(sig.parsedPubKey)) {
			return false
		}
	}
	return true
}

type sortOutsData []Output

func (outs sortOutsData) Less(i, j int) bool {
	c := Codec{}
	iBytes, _ := c.MarshalOutput(outs[i])
	jBytes, _ := c.MarshalOutput(outs[j])
	return bytes.Compare(iBytes, jBytes) == -1
}
func (outs sortOutsData) Len() int      { return len(outs) }
func (outs sortOutsData) Swap(i, j int) { outs[j], outs[i] = outs[i], outs[j] }

// SortOuts sorts the tx output list by byte representation
func SortOuts(outs []Output)          { sort.Sort(sortOutsData(outs)) }
func isSortedOuts(outs []Output) bool { return sort.IsSorted(sortOutsData(outs)) }
