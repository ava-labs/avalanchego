// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
)

var errL1Balance = errors.New("L1 validator balance is not what the continuous fee leaves")

// An l1Account is what an L1 validator has left to pay its continuous fee
// with, as of a chain time.
//
// ACP-77: each active L1 validator pays the validator fee price for each
// second of chain time, out of its balance, and the chain deactivates it once
// the balance cannot pay. A conversion starts the balance at the chain time of
// its block, an increase adds to it, and a disable refunds what is left. So
// the balance at any chain time follows from those and the price, with no
// copy of how the chain stores it.
type l1Account struct {
	balance uint64
	at      uint64 // chain time, Unix seconds
}

// drain returns a at chain time now, after paying price for each second since
// a.at. A validator whose balance runs out has a balance of 0.
func (a l1Account) drain(now, price uint64) l1Account {
	if now <= a.at {
		return a
	}

	cost, err := math.Mul(now-a.at, price)
	if err != nil || cost >= a.balance {
		return l1Account{at: now}
	}

	return l1Account{balance: a.balance - cost, at: now}
}

// checkL1Balances returns errL1Balance if an L1 validator in accounts does not
// have, in v, the balance that the continuous fee leaves it at v's chain time,
// at price per second. It then sets each account to what v lists.
//
// The price is the one the last check read: it moves with the fee excess,
// which grows by at most the capacity less the target each second, and on the
// test network it would take days to double (nodes.go setTestDefaults). A
// price that moved within a run is a bug in itself.
func checkL1Balances(accounts map[ids.ID]l1Account, v view, price uint64) error {
	var err error
	for _, validationID := range slices.SortedFunc(maps.Keys(accounts), ids.ID.Compare) {
		want := accounts[validationID].drain(v.time, price)
		if got := v.l1[validationID]; got != want.balance && err == nil {
			err = fmt.Errorf("%w: validator %s has %d at chain time %d, and %d at %d less %d a second leaves %d", errL1Balance, validationID, got, v.time, accounts[validationID].balance, accounts[validationID].at, price, want.balance)
		}
		accounts[validationID] = l1Account{balance: v.l1[validationID], at: v.time}
	}

	return err
}
