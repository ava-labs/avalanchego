// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

const MaxSignatures = 256

var (
	errTooManySignatures = errors.New("too many signatures")
	errCyclicAliases     = errors.New("cyclic aliases not allowed")
)

// Spend attempts to create an input from outputowners which can contain multisig aliases
func (kc *Keychain) SpendMultiSig(out verify.Verifiable, time uint64, msigIntf interface{}) (
	verify.Verifiable, []*crypto.PrivateKeySECP256K1R, error,
) {
	if len(kc.Keys) == 0 {
		return nil, nil, errCantSpend
	}

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
	keys := make([]*crypto.PrivateKeySECP256K1R, 0, owners.Threshold)

	tf := func(_ bool, addr ids.ShortID, depth int, visited, _, _ uint32) (bool, error) {
		if key, exists := kc.get(addr); exists {
			if depth == 1 {
				sigs = append(sigs, visited)
			}
			keys = append(keys, key)
			// if alias is true, this prevents next level
			return true, nil
		}
		return false, nil
	}

	if _, err := TraverseOwners(owners, msig, tf); err != nil {
		return nil, nil, err
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

type TraverserOwnerFunc func(
	alias bool,
	addr ids.ShortID,
	depth int,
	visited,
	verified,
	totalVisited uint32,
) (bool, error)

// TraverseOwners traverses through owners, visits every address and callbacks in case a
// non-multisig address is visited. Nested multisig alias are excluded from sigIndex concept.
// The sigIndex(es) on base level must be set as MaxUint32
func TraverseOwners(out *OutputOwners, msig AliasGetter, callback TraverserOwnerFunc) (uint32, error) {
	var visited, verified uint32

	type stackItem struct {
		index, verified uint32
		owners          *OutputOwners
	}

	cycleCheck := set.Set[ids.ShortID]{}
	stack := []*stackItem{{owners: out}}
	for len(stack) > 0 {
	Stack:
		// get head
		currentStack := stack[len(stack)-1]
		for int(currentStack.index) < len(currentStack.owners.Addrs) &&
			currentStack.verified < currentStack.owners.Threshold {
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
					return 0, errWrongOwnerType
				}
				success, err := callback(
					true,
					addr,
					len(stack),
					currentStack.index-1,
					currentStack.verified,
					visited,
				)
				if err != nil {
					return 0, err
				}

				if success {
					currentStack.verified++
					verified++
				} else {
					stack = append(stack, &stackItem{owners: owners})
					goto Stack
				}
			case database.ErrNotFound: // non-multi-sig
				if visited > MaxSignatures {
					return 0, errTooManySignatures
				}
				success, err := callback(
					false,
					addr,
					len(stack),
					currentStack.index-1,
					currentStack.verified,
					visited,
				)
				if err != nil {
					return 0, err
				}
				if success {
					currentStack.verified++
					verified++
				}
				visited++
			default:
				return 0, err
			}
		}
		// verify current level
		if currentStack.verified < currentStack.owners.Threshold {
			return 0, errCantSpend
		}
		// remove head
		stack = stack[:len(stack)-1]
		// apply child verification
		if len(stack) > 0 {
			stack[len(stack)-1].verified++
		}
	}
	return verified, nil
}
