// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/formatting"
)

var (
	errNilID = errors.New("nil ID is not valid")
)

// Service defines the API services exposed by the spdag vm
type Service struct{ vm *VM }

// IssueTxArgs are arguments for passing into IssueTx requests
type IssueTxArgs struct {
	Tx formatting.CB58 `json:"tx"`
}

// IssueTxReply defines the IssueTx replies returned from the API
type IssueTxReply struct {
	TxID ids.ID `json:"txID"`
}

// IssueTx attempts to issue a transaction into consensus
func (service *Service) IssueTx(r *http.Request, args *IssueTxArgs, reply *IssueTxReply) error {
	service.vm.ctx.Log.Verbo("IssueTx called with %s", args.Tx)

	txID, err := service.vm.IssueTx(args.Tx.Bytes, nil)
	if err != nil {
		service.vm.ctx.Log.Debug("IssueTx failed to issue due to %s", err)
		return err
	}

	reply.TxID = txID
	return nil
}

// GetTxStatusArgs are arguments for GetTxStatus
type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
}

// GetTxStatusReply is the reply from GetTxStatus
type GetTxStatusReply struct {
	// Status of the returned transaction
	Status choices.Status `json:"status"`
}

// GetTxStatus returns the status of the transaction whose ID is [args.TxID]
func (service *Service) GetTxStatus(r *http.Request, args *GetTxStatusArgs, reply *GetTxStatusReply) error {
	service.vm.ctx.Log.Verbo("GetTxStatus called with %s", args.TxID)

	if args.TxID.IsZero() {
		return errNilID
	}

	tx := UniqueTx{
		vm:   service.vm,
		txID: args.TxID,
	}

	reply.Status = tx.Status()
	return nil
}

// GetUTXOsArgs are arguments for GetUTXOs
type GetUTXOsArgs struct {
	Addresses []ids.ShortID `json:"addresses"`
}

// GetUTXOsReply is the reply from GetUTXOs
type GetUTXOsReply struct {
	// Each element is the string repr. of an unspent UTXO that
	// references an address in the arguments
	UTXOs []formatting.CB58 `json:"utxos"`
}

// GetUTXOs returns the UTXOs such that at least one address in [args.Addresses]
// is referenced in the UTXO.
func (service *Service) GetUTXOs(r *http.Request, args *GetUTXOsArgs, reply *GetUTXOsReply) error {
	service.vm.ctx.Log.Verbo("GetUTXOs called with %s", args.Addresses)

	addrSet := ids.ShortSet{}
	for _, addr := range args.Addresses {
		if addr.IsZero() {
			return errNilID
		}
	}
	addrSet.Add(args.Addresses...)

	utxos, err := service.vm.GetUTXOs(addrSet)
	if err != nil {
		return err
	}

	reply.UTXOs = []formatting.CB58{}
	for _, utxo := range utxos {
		reply.UTXOs = append(reply.UTXOs, formatting.CB58{Bytes: utxo.Bytes()})
	}
	return nil
}
