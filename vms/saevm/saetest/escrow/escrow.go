// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The above copyright and licensing exclude the original Escrow.sol contract
// and compiled artefacts, which are licensed under the following:
//
// Copyright 2024 Divergence Tech Ltd.

// Package escrow provides bytecode and helpers for the [Escrow.sol] contract
// deployed to 0xf92186Fc58dA366431e98f3Ddd563d0A3cdf4f59 on Ethereum mainnet.
//
// [Escrow.sol]: https://github.com/ARR4N/SWAP2/blob/fe724e87bdc998c3b497c16e35fed354e53dc3e9/src/Escrow.sol
package escrow

import (
	"slices"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	creation = "0x6080806040523460155761029e908161001a8239f35b5f80fdfe6040608081526004361015610012575f80fd5b5f3560e01c80633ccfd60b1461017757806351cff8d914610148578063837b2d1d1461010e578063e3d670d7146100d35763f340fa0114610051575f80fd5b60203660031901126100cf576004356001600160a01b03811691908290036100cf57815f525f602052805f209182543481018091116100bb577fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c93558151908152346020820152a1005b634e487b7160e01b5f52601160045260245ffd5b5f80fd5b50346100cf5760203660031901126100cf576004356001600160a01b03811691908290036100cf576020915f525f8252805f20549051908152f35b50346100cf575f3660031901126100cf57602090517fe3f9c77ea5446c989d214acc27cefc902862791ee093b44540c8790a484451828152f35b346100cf5760203660031901126100cf576004356001600160a01b03811681036100cf576101759061018c565b005b346100cf575f3660031901126100cf57610175335b60018060a01b0316805f525f602052604090815f2054801561027a57815f525f6020525f83812055804710610263575f80808084865af13d1561025e5767ffffffffffffffff3d81811161024a57855191601f8201601f19908116603f011683019081118382101761024a57865281525f60203d92013e5b1561023957825191825260208201527f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b659190a1565b8251630a12f52160e11b8152600490fd5b634e487b7160e01b5f52604160045260245ffd5b610204565b825163cd78605960e01b8152306004820152602490fd5b5060249151906316b4356760e31b82526004820152fdfea164736f6c6343000819000a"
	deployed = "0x6040608081526004361015610012575f80fd5b5f3560e01c80633ccfd60b1461017757806351cff8d914610148578063837b2d1d1461010e578063e3d670d7146100d35763f340fa0114610051575f80fd5b60203660031901126100cf576004356001600160a01b03811691908290036100cf57815f525f602052805f209182543481018091116100bb577fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c93558151908152346020820152a1005b634e487b7160e01b5f52601160045260245ffd5b5f80fd5b50346100cf5760203660031901126100cf576004356001600160a01b03811691908290036100cf576020915f525f8252805f20549051908152f35b50346100cf575f3660031901126100cf57602090517fe3f9c77ea5446c989d214acc27cefc902862791ee093b44540c8790a484451828152f35b346100cf5760203660031901126100cf576004356001600160a01b03811681036100cf576101759061018c565b005b346100cf575f3660031901126100cf57610175335b60018060a01b0316805f525f602052604090815f2054801561027a57815f525f6020525f83812055804710610263575f80808084865af13d1561025e5767ffffffffffffffff3d81811161024a57855191601f8201601f19908116603f011683019081118382101761024a57865281525f60203d92013e5b1561023957825191825260208201527f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b659190a1565b8251630a12f52160e11b8152600490fd5b634e487b7160e01b5f52604160045260245ffd5b610204565b825163cd78605960e01b8152306004820152602490fd5b5060249151906316b4356760e31b82526004820152fdfea164736f6c6343000819000a"
)

// CreationCode returns the EVM bytecode for deploying the Escrow.sol contract.
func CreationCode() []byte {
	return common.FromHex(creation)
}

// ByteCode returns the deployed EVM bytecode of the Escrow.sol contract.
func ByteCode() []byte {
	return common.FromHex(deployed)
}

const abiJSON = `[{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"AddressInsufficientBalance","type":"error"},{"inputs":[],"name":"FailedInnerCall","type":"error"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"ZeroBalance","type":"error"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"","type":"address"},{"indexed":false,"internalType":"uint256","name":"","type":"uint256"}],"name":"Deposit","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"","type":"address"},{"indexed":false,"internalType":"uint256","name":"","type":"uint256"}],"name":"Withdrawal","type":"event"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"balance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address payable","name":"account","type":"address"}],"name":"deposit","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[],"name":"isEscrow","outputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"stateMutability":"pure","type":"function"},{"inputs":[],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

// ABI returns a freshly parsed Escrow.sol ABI.
func ABI(tb testing.TB) abi.ABI {
	tb.Helper()

	a, err := abi.JSON(strings.NewReader(abiJSON))
	require.NoError(tb, err, "abi.JSON([Escrow.sol])")
	return a
}

// CallDataToDeposit returns the transaction call data to deposit native token
// for the given recipient.
func CallDataToDeposit(recipient common.Address) []byte {
	return callDataWithAddr("deposit(address)", recipient)
}

// CallDataForBalance returns the transaction call data to retrieve the balance
// in escrow for the given beneficiary.
func CallDataForBalance(beneficiary common.Address) []byte {
	return callDataWithAddr("balance(address)", beneficiary)
}

// StorageKeyForBalance returns the storage slot holding balances[beneficiary],
// the mapping at slot 0: keccak256(abi.encode(beneficiary, uint256(0))).
func StorageKeyForBalance(beneficiary common.Address) common.Hash {
	return crypto.Keccak256Hash(
		common.LeftPadBytes(beneficiary.Bytes(), 32),
		common.Hash{}.Bytes(),
	)
}

func callDataWithAddr(sig string, addr common.Address) []byte {
	return slices.Concat(
		crypto.Keccak256([]byte(sig))[:4],
		make([]byte, 12), addr[:],
	)
}

// CallDataToWithdraw returns the transaction call data to withdraw the
// caller's escrowed balance.
func CallDataToWithdraw() []byte {
	return crypto.Keccak256([]byte("withdraw()"))[:4]
}

// DepositEvent returns the [types.Log] emitted by a successful transaction with
// [CallDataToDeposit] data. It is equivalent to passing an empty log to
// [WithDepositTopicsAndData].
func DepositEvent(recipient common.Address, amount *uint256.Int) *types.Log {
	return WithDepositTopicsAndData(new(types.Log), recipient, amount)
}

// WithDepositTopicsAndData populates the [types.Log.Topics] and
// [types.Log.Data] fields of the provided log with those emitted by a
// successful transaction with [CallDataToDeposit] data.
//
// The received log is modified and then returned for convenience.
func WithDepositTopicsAndData(log *types.Log, recipient common.Address, amount *uint256.Int) *types.Log {
	log.Topics = []common.Hash{crypto.Keccak256Hash([]byte("Deposit(address,uint256)"))}
	log.Data = slices.Concat(
		make([]byte, 12), recipient[:],
		amount.PaddedBytes(32),
	)
	return log
}
