// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type WalletService struct {
	vm         *VM
	pendingTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
}

func (w *WalletService) decided(txID ids.ID) {
	w.pendingTxs.Delete(txID)
}

func (w *WalletService) issue(txBytes []byte) (ids.ID, error) {
	tx, err := w.vm.parser.ParseTx(txBytes)
	if err != nil {
		return ids.ID{}, err
	}

	txID, err := w.vm.IssueTx(txBytes)
	if err != nil {
		return ids.ID{}, err
	}

	if _, ok := w.pendingTxs.Get(txID); !ok {
		w.pendingTxs.Put(txID, tx)
	}

	return txID, nil
}

func (w *WalletService) update(utxos []*avax.UTXO) ([]*avax.UTXO, error) {
	utxoMap := make(map[ids.ID]*avax.UTXO, len(utxos))
	for _, utxo := range utxos {
		utxoMap[utxo.InputID()] = utxo
	}

	iter := w.pendingTxs.NewIterator()

	for iter.Next() {
		tx := iter.Value()
		for _, inputUTXO := range tx.Unsigned.InputUTXOs() {
			if inputUTXO.Symbolic() {
				continue
			}
			utxoID := inputUTXO.InputID()
			if _, exists := utxoMap[utxoID]; !exists {
				return nil, errMissingUTXO
			}
			delete(utxoMap, utxoID)
		}

		for _, utxo := range tx.UTXOs() {
			utxoMap[utxo.InputID()] = utxo
		}
	}

	return maps.Values(utxoMap), nil
}

// IssueTx attempts to issue a transaction into consensus
func (w *WalletService) IssueTx(_ *http.Request, args *api.FormattedTx, reply *api.JSONTxID) error {
	w.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "wallet"),
		zap.String("method", "issueTx"),
		logging.UserString("tx", args.Tx),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	txID, err := w.issue(txBytes)
	reply.TxID = txID
	return err
}

// Send returns the ID of the newly created transaction
func (w *WalletService) Send(r *http.Request, args *SendArgs, reply *api.JSONTxIDChangeAddr) error {
	return w.SendMultiple(r, &SendMultipleArgs{
		JSONSpendHeader: args.JSONSpendHeader,
		Outputs:         []SendOutput{args.SendOutput},
		Memo:            args.Memo,
	}, reply)
}

// SendMultiple sends a transaction with multiple outputs.
func (w *WalletService) SendMultiple(_ *http.Request, args *SendMultipleArgs, reply *api.JSONTxIDChangeAddr) error {
	w.vm.ctx.Log.Warn("deprecated API called",
		zap.String("service", "wallet"),
		zap.String("method", "sendMultiple"),
		logging.UserString("username", args.Username),
	)

	// Validate the memo field
	memoBytes := []byte(args.Memo)
	if l := len(memoBytes); l > avax.MaxMemoSize {
		return fmt.Errorf("max memo length is %d but provided memo field is length %d",
			avax.MaxMemoSize,
			l)
	} else if len(args.Outputs) == 0 {
		return errNoOutputs
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(w.vm, args.From)
	if err != nil {
		return fmt.Errorf("couldn't parse 'From' addresses: %w", err)
	}

	// Load user's UTXOs/keys
	utxos, kc, err := w.vm.LoadUser(args.Username, args.Password, fromAddrs)
	if err != nil {
		return err
	}

	utxos, err = w.update(utxos)
	if err != nil {
		return err
	}

	// Parse the change address.
	if len(kc.Keys) == 0 {
		return errNoKeys
	}
	changeAddr, err := w.vm.selectChangeAddr(kc.Keys[0].PublicKey().Address(), args.ChangeAddr)
	if err != nil {
		return err
	}

	// Calculate required input amounts and create the desired outputs
	// String repr. of asset ID --> asset ID
	assetIDs := make(map[string]ids.ID)
	// Asset ID --> amount of that asset being sent
	amounts := make(map[ids.ID]uint64)
	// Outputs of our tx
	outs := []*avax.TransferableOutput{}
	for _, output := range args.Outputs {
		if output.Amount == 0 {
			return errZeroAmount
		}
		assetID, ok := assetIDs[output.AssetID] // Asset ID of next output
		if !ok {
			assetID, err = w.vm.lookupAssetID(output.AssetID)
			if err != nil {
				return fmt.Errorf("couldn't find asset %s", output.AssetID)
			}
			assetIDs[output.AssetID] = assetID
		}
		currentAmount := amounts[assetID]
		newAmount, err := math.Add64(currentAmount, uint64(output.Amount))
		if err != nil {
			return fmt.Errorf("problem calculating required spend amount: %w", err)
		}
		amounts[assetID] = newAmount

		// Parse the to address
		to, err := avax.ParseServiceAddress(w.vm, output.To)
		if err != nil {
			return fmt.Errorf("problem parsing to address %q: %w", output.To, err)
		}

		// Create the Output
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(output.Amount),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	amountsWithFee := maps.Clone(amounts)

	amountWithFee, err := math.Add64(amounts[w.vm.feeAssetID], w.vm.TxFee)
	if err != nil {
		return fmt.Errorf("problem calculating required spend amount: %w", err)
	}
	amountsWithFee[w.vm.feeAssetID] = amountWithFee

	amountsSpent, ins, keys, err := w.vm.Spend(
		utxos,
		kc,
		amountsWithFee,
	)
	if err != nil {
		return err
	}

	// Add the required change outputs
	for assetID, amountWithFee := range amountsWithFee {
		amountSpent := amountsSpent[assetID]

		if amountSpent > amountWithFee {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amountSpent - amountWithFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}
	}

	codec := w.vm.parser.Codec()
	avax.SortTransferableOutputs(outs, codec)

	tx := txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    w.vm.ctx.NetworkID,
		BlockchainID: w.vm.ctx.ChainID,
		Outs:         outs,
		Ins:          ins,
		Memo:         memoBytes,
	}}}
	if err := tx.SignSECP256K1Fx(codec, keys); err != nil {
		return err
	}

	txID, err := w.issue(tx.Bytes())
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	reply.ChangeAddr, err = w.vm.FormatLocalAddress(changeAddr)
	return err
}
