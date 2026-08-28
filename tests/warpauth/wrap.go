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
	cchaintx "github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errShortPayload = errors.New("warp payload shorter than an owner address")
	errNotImport    = errors.New("C-chain tx is not an import")
)

// Wrap rebuilds the P-chain tx carried by a signed warp message and attaches
// the message as the credential for every input and authorization. It also
// returns the owner named in the payload.
func Wrap(signedMessage []byte) (*txs.Tx, ids.ShortID, error) {
	msg, err := warp.ParseMessage(signedMessage)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	call, err := payload.ParseAddressedCall(msg.Payload)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	if len(call.Payload) < ids.ShortIDLen {
		return nil, ids.ShortEmpty, errShortPayload
	}
	owner := ids.ShortID(call.Payload[:ids.ShortIDLen])
	var unsigned txs.UnsignedTx
	if _, err := txs.Codec.Unmarshal(call.Payload[ids.ShortIDLen:], &unsigned); err != nil {
		return nil, ids.ShortEmpty, fmt.Errorf("parsing tx from warp payload: %w", err)
	}

	// InputIDs includes an ImportTx's imported inputs.
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
	return tx, owner, tx.Initialize(txs.Codec)
}

// exportPayloadLen is owner || nAVAX amount: an export to the P-chain, which
// the C-chain executes by itself.
const exportPayloadLen = 20 + 8

// WrapCChain rebuilds the C-chain ImportTx carried by an unsigned warp
// message and attaches the message, with an empty signature, as the
// credential for every imported input. The C-chain verifies the message
// against its own log, so no BLS signatures are needed.
func WrapCChain(unsigned *warp.UnsignedMessage) (*cchaintx.Tx, ids.ShortID, error) {
	call, err := payload.ParseAddressedCall(unsigned.Payload)
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	if len(call.Payload) < ids.ShortIDLen {
		return nil, ids.ShortEmpty, errShortPayload
	}
	owner := ids.ShortID(call.Payload[:ids.ShortIDLen])
	unsignedTx, err := cchaintx.ParseUnsigned(call.Payload[ids.ShortIDLen:])
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	importTx, ok := unsignedTx.(*cchaintx.Import)
	if !ok {
		return nil, ids.ShortEmpty, fmt.Errorf("%w: %T", errNotImport, unsignedTx)
	}
	msg, err := warp.NewMessage(unsigned, &warp.BitSetSignature{})
	if err != nil {
		return nil, ids.ShortEmpty, err
	}
	creds := make([]cchaintx.Credential, len(importTx.ImportedInputs))
	for i := range creds {
		creds[i] = &secp256k1fx.WarpCredential{Message: msg.Bytes()}
	}
	return &cchaintx.Tx{Unsigned: importTx, Creds: creds}, owner, nil
}
