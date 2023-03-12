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

type TFResult int

const (
	TFVerify TFResult = iota
	TFSkip
	TFContinue
	TFError
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

	tf := func(alias bool, addr ids.ShortID, visited, _, totalVisited, totalVerified uint32) (TFResult, error) {
		if key, exists := kc.get(addr); exists {
			// Remove signatures from failed children
			if totalVerified < uint32(len(sigs)) {
				sigs = sigs[:totalVerified]
				keys = keys[:totalVerified]
			}
			sigs = append(sigs, totalVisited)
			keys = append(keys, key)
			// For alias -> skip children
			return TFVerify, nil
		}
		// Assumption we traverse with real addresses -> process children
		return TFContinue, nil
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

type TraverserOwnerFunc func(
	alias bool,
	addr ids.ShortID,
	visited,
	verified,
	totalVisited,
	totalVerified uint32,
) (TFResult, error)

// TraverseOwners traverses through owners, visits every address and callbacks in case a
// non-multisig address is visited. Nested multisig alias are excluded from sigIndex concept.
// The sigIndex(es) on base level must be set as MaxUint32
func TraverseOwners(out *OutputOwners, msig AliasGetter, callback TraverserOwnerFunc) (uint32, error) {
	var visited, verified uint32

	type stackItem struct {
		index, verified, verifiedTotal, visitedTotal uint32
		owners                                       *OutputOwners
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
				result, err := callback(
					true,
					addr,
					currentStack.index-1,
					currentStack.verified,
					visited,
					verified,
				)
				if err != nil {
					return 0, err
				}

				switch result {
				case TFVerify:
					currentStack.verified++
					verified++
				case TFContinue:
					stack = append(stack, &stackItem{owners: owners, verifiedTotal: verified, visitedTotal: visited})
					goto Stack
				}
				visited++
			case database.ErrNotFound: // non-multi-sig
				if visited > MaxSignatures {
					return 0, errTooManySignatures
				}
				result, err := callback(
					false,
					addr,
					currentStack.index-1,
					currentStack.verified,
					visited,
					verified,
				)
				if err != nil {
					return 0, err
				}
				if result == TFVerify {
					currentStack.verified++
					verified++
				}
				visited++
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
			verified = currentStack.verifiedTotal
			visited = currentStack.visitedTotal
		} else if len(stack) > 0 {
			// apply child verification
			stack[len(stack)-1].verified++
		}
	}
	return verified, nil
}
