// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	errCalledInitialize      = errors.New("unexpectedly called Initialize")
	errCalledBootstrapping   = errors.New("unexpectedly called Bootstrapping")
	errCalledBootstrapped    = errors.New("unexpectedly called Bootstrapped")
	errCalledVerifyTransfer  = errors.New("unexpectedly called VerifyTransfer")
	errCalledVerifyOperation = errors.New("unexpectedly called VerifyOperation")
)

type FxTest struct {
	T *testing.T

	CantInitialize,
	CantBootstrapping,
	CantBootstrapped,
	CantVerifyTransfer,
	CantVerifyOperation bool

	InitializeF      func(vm interface{}) error
	BootstrappingF   func() error
	BootstrappedF    func() error
	VerifyTransferF  func(tx, in, cred, utxo interface{}) error
	VerifyOperationF func(tx, op, cred interface{}, utxos []interface{}) error
}

func (fx *FxTest) Default(cant bool) {
	fx.CantInitialize = cant
	fx.CantBootstrapping = cant
	fx.CantBootstrapped = cant
	fx.CantVerifyTransfer = cant
	fx.CantVerifyOperation = cant
}

func (fx *FxTest) Initialize(vm interface{}) error {
	if fx.InitializeF != nil {
		return fx.InitializeF(vm)
	}
	if !fx.CantInitialize {
		return nil
	}
	if fx.T != nil {
		require.FailNow(fx.T, errCalledInitialize.Error())
	}
	return errCalledInitialize
}

func (fx *FxTest) Bootstrapping() error {
	if fx.BootstrappingF != nil {
		return fx.BootstrappingF()
	}
	if !fx.CantBootstrapping {
		return nil
	}
	if fx.T != nil {
		require.FailNow(fx.T, errCalledBootstrapping.Error())
	}
	return errCalledBootstrapping
}

func (fx *FxTest) Bootstrapped() error {
	if fx.BootstrappedF != nil {
		return fx.BootstrappedF()
	}
	if !fx.CantBootstrapped {
		return nil
	}
	if fx.T != nil {
		require.FailNow(fx.T, errCalledBootstrapped.Error())
	}
	return errCalledBootstrapped
}

func (fx *FxTest) VerifyTransfer(tx, in, cred, utxo interface{}) error {
	if fx.VerifyTransferF != nil {
		return fx.VerifyTransferF(tx, in, cred, utxo)
	}
	if !fx.CantVerifyTransfer {
		return nil
	}
	if fx.T != nil {
		require.FailNow(fx.T, errCalledVerifyTransfer.Error())
	}
	return errCalledVerifyTransfer
}

func (fx *FxTest) VerifyOperation(tx, op, cred interface{}, utxos []interface{}) error {
	if fx.VerifyOperationF != nil {
		return fx.VerifyOperationF(tx, op, cred, utxos)
	}
	if !fx.CantVerifyOperation {
		return nil
	}
	if fx.T != nil {
		require.FailNow(fx.T, errCalledVerifyOperation.Error())
	}
	return errCalledVerifyOperation
}
