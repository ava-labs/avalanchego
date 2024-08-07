// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

const MaxSignatures = 256

var (
	errTooManySignatures = errors.New("too many signatures")
	errCyclicAliases     = errors.New("cyclic aliases not allowed")
)

// SpendMultisig attempts to create an input from outputowners which can contain multisig aliases
// Multisig alias are resolved into addresses and the sigIdxs returned are counted global.
// Multisig occurrences itself do not count in any way in sigIdxs, only addresses do.
func (kc *Keychain) SpendMultiSig(
	out verify.Verifiable,
	time uint64,
	msigIntf interface{},
) (
	verify.Verifiable, []*secp256k1.PrivateKey, error,
) {
	// get the multisig alias getter
	msig, ok := msigIntf.(AliasGetter)
	if !ok {
		return kc.Spend(out, time)
	}

	var owners *OutputOwners
	switch out := out.(type) {
	case *MintOutput:
		owners = &out.OutputOwners
	case *TransferOutput:
		owners = &out.OutputOwners
	default:
		return nil, nil, errWrongOutputType
	}

	// assume non-multisigs for reserving memory
	sigs := make([]uint32, 0, owners.Threshold)
	keys := make([]*secp256k1.PrivateKey, 0, owners.Threshold)

	tf := func(addr ids.ShortID, totalVisited, totalVerified uint32) (bool, error) {
		if key, exists := kc.get(addr); exists {
			// In case a nested alias doesnt meet threshold,
			if totalVerified < uint32(len(sigs)) {
				sigs = sigs[:totalVerified]
				keys = keys[:totalVerified]
			}
			sigs = append(sigs, totalVisited)
			keys = append(keys, key)
			// Verified address
			return true, nil
		}
		// This address cannot be verified
		return false, nil
	}

	totalVerified, err := TraverseOwners(owners, msig, tf)
	if err != nil {
		return nil, nil, err
	}

	if totalVerified < uint32(len(sigs)) {
		sigs = sigs[:totalVerified]
		keys = keys[:totalVerified]
	}

	switch out := out.(type) {
	case *MintOutput:
		return &Input{
			SigIndices: sigs,
		}, keys, nil
	default:
		return &TransferInput{
			Amt: out.(*TransferOutput).Amt,
			Input: Input{
				SigIndices: sigs,
			},
		}, keys, nil
	}
}

type TraverseOwnerFunc func(
	addr ids.ShortID,
	totalVisited,
	totalVerified uint32,
) (bool, error)

type TraverseAliasFunc func(
	alias *multisig.AliasWithNonce,
)

// TraverseOwners traverses through owners, visits every address and callbacks in case a
// non-multisig address is visited. Nested multisig alias are excluded from sigIndex concept.
func TraverseOwners(out *OutputOwners, msig AliasGetter, callback TraverseOwnerFunc) (uint32, error) {
	var addrVisited, addrVerified uint32

	type stackItem struct {
		index,
		verified,
		addrVerifiedTotal uint32
		parentVerified bool
		owners         *OutputOwners
	}

	cycleCheck := set.Set[ids.ShortID]{}
	stack := []*stackItem{{owners: out}}
	for len(stack) > 0 {
	Stack:
		// get head
		currentStack := stack[len(stack)-1]
		for int(currentStack.index) < len(currentStack.owners.Addrs) {
			// get the next address to check
			addr := currentStack.owners.Addrs[currentStack.index]
			currentStack.index++
			// Is it a multi-sig address ?
			alias, err := msig.GetMultisigAlias(addr)
			switch err {
			case nil: // multi-sig
				if len(stack) > MaxSignatures {
					return 0, errTooManySignatures
				}
				if cycleCheck.Contains(addr) {
					return 0, errCyclicAliases
				}
				cycleCheck.Add(addr)
				owners, ok := alias.Owners.(*OutputOwners)
				if !ok {
					return 0, ErrWrongOwnerType
				}
				stack = append(stack, &stackItem{
					owners:            owners,
					addrVerifiedTotal: addrVerified,
					parentVerified:    currentStack.parentVerified || currentStack.verified >= currentStack.owners.Threshold,
				})
				goto Stack
			case database.ErrNotFound: // non-multi-sig
				if !currentStack.parentVerified && currentStack.verified < currentStack.owners.Threshold {
					success, err := callback(
						addr,
						addrVisited,
						addrVerified,
					)
					if err != nil {
						return 0, err
					}
					if success {
						currentStack.verified++
						addrVerified++

						if addrVerified > MaxSignatures {
							return 0, errTooManySignatures
						}
					}
				}
				addrVisited++
			default:
				return 0, err
			}
		}

		// remove head
		stack = stack[:len(stack)-1]
		// verify current level
		if currentStack.verified < currentStack.owners.Threshold {
			if len(stack) == 0 {
				return 0, errCantSpend
			}
			// We recover to previous state
			addrVerified = currentStack.addrVerifiedTotal
		} else if len(stack) > 0 {
			currentStack = stack[len(stack)-1]
			if currentStack.verified < currentStack.owners.Threshold {
				// apply child verification
				currentStack.verified++
			}
		}
	}
	return addrVerified, nil
}

func TraverseAliases(out *OutputOwners, msig AliasGetter, callback TraverseAliasFunc) error {
	type stackItem struct {
		index  int
		owners *OutputOwners
	}

	cycleCheck := set.Set[ids.ShortID]{}
	stack := []*stackItem{{owners: out}}
	for len(stack) > 0 {
	Stack:
		// get head
		currentStack := stack[len(stack)-1]
		for currentStack.index < len(currentStack.owners.Addrs) {
			// get the next address to check
			addr := currentStack.owners.Addrs[currentStack.index]
			currentStack.index++
			// Is it a multi-sig address ?
			alias, err := msig.GetMultisigAlias(addr)
			switch err {
			case nil: // multi-sig
				if len(stack) > MaxSignatures {
					return errTooManySignatures
				}
				if cycleCheck.Contains(addr) {
					return errCyclicAliases
				}
				cycleCheck.Add(addr)

				owners, ok := alias.Owners.(*OutputOwners)
				if !ok {
					return ErrWrongOwnerType
				}
				stack = append(stack, &stackItem{
					owners: owners,
				})
				callback(alias)
				goto Stack
			case database.ErrNotFound: // non-multi-sig
				// Normal address, nothing to do
			default:
				return err
			}
		}

		// remove head
		stack = stack[:len(stack)-1]
	}
	return nil
}
