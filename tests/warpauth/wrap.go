// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errShortPayload = errors.New("warp payload shorter than an owner address")

// Wrap rebuilds the P-chain tx carried by a signed warp message and attaches
// the message as the credential for every input and authorization. This is
// all a relayer has to do.
func Wrap(signedMessage []byte) (*txs.Tx, error) {
	msg, err := warp.ParseMessage(signedMessage)
	if err != nil {
		return nil, err
	}
	call, err := payload.ParseAddressedCall(msg.Payload)
	if err != nil {
		return nil, err
	}
	if len(call.Payload) < ids.ShortIDLen {
		return nil, errShortPayload
	}
	var unsigned txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(call.Payload[ids.ShortIDLen:], &unsigned); err != nil {
		return nil, fmt.Errorf("parsing tx from warp payload: %w", err)
	}

	numCreds := len(unsigned.InputIDs())
	switch unsigned.(type) {
	case *txs.CreateChainTx, *txs.AddSubnetValidatorTx, *txs.RemoveSubnetValidatorTx,
		*txs.TransferSubnetOwnershipTx, *txs.ConvertSubnetToL1Tx,
		*txs.DisableL1ValidatorTx, *txs.SetAutoRenewedValidatorConfigTx:
		numCreds++
	}
	creds := make([]verify.Verifiable, numCreds)
	for i := range creds {
		creds[i] = &secp256k1fx.WarpCredential{Message: signedMessage}
	}
	tx := &txs.Tx{Unsigned: unsigned, Creds: creds}
	return tx, tx.Initialize(txs.Codec)
}
