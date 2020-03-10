// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
)

// Service defines the API exposed by the payments vm
type Service struct{ vm *VM }

// IssueTxArgs are the arguments for IssueTx.
// [Tx] is the string representation of the transaction being issued
type IssueTxArgs struct {
	Tx formatting.CB58 `json:"tx"`
}

// IssueTxReply is the reply from IssueTx
// [TxID] is the ID of the issued transaction.
type IssueTxReply struct {
	TxID ids.ID `json:"txID"`
}

// IssueTx issues the transaction specified in [args] to this service
func (service *Service) IssueTx(_ *http.Request, args *IssueTxArgs, reply *IssueTxReply) error {
	service.vm.ctx.Log.Verbo("IssueTx called with args: %s", args.Tx)

	// Issue the tx
	txID, err := service.vm.IssueTx(args.Tx.Bytes, nil)
	if err != nil {
		return err
	}

	reply.TxID = txID
	return nil
}

// GetAccountArgs is the arguments for calling GetAccount
// [Address] is the string repr. of the address we want to know the nonce and balance of
type GetAccountArgs struct {
	Address ids.ShortID `json:"address"`
}

// GetAccountReply is the reply from calling GetAccount
// [nonce] is the nonce of the address specified in the arguments.
// [balance] is the balance of the address specified in the arguments.
type GetAccountReply struct {
	Balance json.Uint64 `json:"balance"`
	Nonce   json.Uint64 `json:"nonce"`
}

// GetAccount gets the nonce and balance of the account specified in [args]
func (service *Service) GetAccount(_ *http.Request, args *GetAccountArgs, reply *GetAccountReply) error {
	if args.Address.IsZero() {
		return errInvalidAddress
	}

	account := service.vm.GetAccount(service.vm.baseDB, args.Address)
	reply.Nonce = json.Uint64(account.nonce)
	reply.Balance = json.Uint64(account.balance)
	return nil
}
