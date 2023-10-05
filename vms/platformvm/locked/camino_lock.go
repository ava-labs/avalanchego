// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package locked

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	errInvalidLockState = errors.New("invalid lockState")
	errNestedLocks      = errors.New("shouldn't nest locks")
	ThisTxID            = ids.ID{'t', 'h', 'i', 's', ' ', 't', 'x', ' ', 'i', 'd'}
)

type State byte

const (
	StateUnlocked        State = 0b00
	StateDeposited       State = 0b01
	StateBonded          State = 0b10
	StateDepositedBonded State = 0b11
)

var stateStrings = map[State]string{
	StateUnlocked:        "unlocked",
	StateDeposited:       "deposited",
	StateBonded:          "bonded",
	StateDepositedBonded: "depositedBonded",
}

func (ls State) String() string {
	stateString, ok := stateStrings[ls]
	if !ok {
		return fmt.Sprintf("unknownLockState(%d)", ls)
	}
	return stateString
}

func (ls State) Verify() error {
	if ls < StateUnlocked || StateDepositedBonded < ls {
		return errInvalidLockState
	}
	return nil
}

func (ls State) IsBonded() bool {
	return StateBonded&ls == StateBonded
}

func (ls State) IsDeposited() bool {
	return StateDeposited&ls == StateDeposited
}

func (ls State) IsDepositedBonded() bool {
	return StateDepositedBonded&ls == StateDepositedBonded
}

/**********************  IDs *********************/

type IDs struct {
	DepositTxID ids.ID `serialize:"true" json:"depositTxID"`
	BondTxID    ids.ID `serialize:"true" json:"bondTxID"`
}

var IDsEmpty = IDs{ids.Empty, ids.Empty}

func (lock IDs) LockState() State {
	lockState := StateUnlocked
	if lock.DepositTxID != ids.Empty {
		lockState = StateDeposited
	}
	if lock.BondTxID != ids.Empty {
		lockState |= StateBonded
	}
	return lockState
}

func (lock IDs) Lock(lockState State) IDs {
	if lockState.IsDeposited() {
		lock.DepositTxID = ThisTxID
	}
	if lockState.IsBonded() {
		lock.BondTxID = ThisTxID
	}
	return lock
}

func (lock IDs) Unlock(lockState State) IDs {
	if lockState.IsDeposited() {
		lock.DepositTxID = ids.Empty
	}
	if lockState.IsBonded() {
		lock.BondTxID = ids.Empty
	}
	return lock
}

func (lock *IDs) FixLockID(txID ids.ID, appliedLockState State) {
	switch appliedLockState {
	case StateDeposited:
		if lock.DepositTxID == ThisTxID {
			lock.DepositTxID = txID
		}
	case StateBonded:
		if lock.BondTxID == ThisTxID {
			lock.BondTxID = txID
		}
	}
}

func (lock IDs) IsLocked() bool {
	return lock.DepositTxID != ids.Empty || lock.BondTxID != ids.Empty
}

func (lock IDs) IsLockedWith(lockState State) bool {
	return lock.LockState()&lockState == lockState
}

func (lock IDs) IsNewlyLockedWith(lockState State) bool {
	switch lockState {
	case StateDeposited:
		return lock.DepositTxID == ThisTxID
	case StateBonded:
		return lock.BondTxID == ThisTxID
	case StateDepositedBonded:
		return lock.DepositTxID == ThisTxID && lock.BondTxID == ThisTxID
	}
	return false
}

func (lock *IDs) Match(lockState State, txIDs set.Set[ids.ID]) bool {
	switch lockState {
	case StateDeposited:
		return txIDs.Contains(lock.DepositTxID)
	case StateBonded:
		return txIDs.Contains(lock.BondTxID)
	case StateDepositedBonded:
		return lock.BondTxID == lock.DepositTxID && txIDs.Contains(lock.DepositTxID)
	}
	return false
}

/**********************  In / Out *********************/

type Out struct {
	IDs                  `serialize:"true" json:"lockIDs"`
	avax.TransferableOut `serialize:"true" json:"output"`
}

func (out *Out) Addresses() [][]byte {
	if addressable, ok := out.TransferableOut.(avax.Addressable); ok {
		return addressable.Addresses()
	}
	return nil
}

func (out *Out) Verify() error {
	if _, nested := out.TransferableOut.(*Out); nested {
		return errNestedLocks
	}
	return out.TransferableOut.Verify()
}

// Used in vms/platformvm/txs/executor/camino_tx_executor.go func outputsAreEqual
func (out *Out) Equal(to any) bool {
	toOut, typeAreEq := to.(*Out)
	outEq, innerIsEq := out.TransferableOut.(interface{ Equal(any) bool })
	return typeAreEq && innerIsEq && out.IDs == toOut.IDs && outEq.Equal(toOut.TransferableOut)
}

type In struct {
	IDs                 `serialize:"true" json:"lockIDs"`
	avax.TransferableIn `serialize:"true" json:"input"`
}

func (in *In) Verify() error {
	if _, nested := in.TransferableIn.(*In); nested {
		return errNestedLocks
	}
	return in.TransferableIn.Verify()
}

// Used in vms/platformvm/txs/executor/camino_tx_executor.go func inputsAreEqual
func (in *In) Equal(to any) bool {
	toIn, typeAreEq := to.(*In)
	inEq, innerIsEq := in.TransferableIn.(interface{ Equal(any) bool })
	return typeAreEq && innerIsEq && in.IDs == toIn.IDs && inEq.Equal(toIn.TransferableIn)
}
