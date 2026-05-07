// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/stretchr/testify/require"
)

// These identifiers are all exported for usage by tx_test.go, which is compiled
// in a separate package to allow for the usage of the txtest package.

const X2CRate = _x2cRate

var (
	ScaleAVAX = scaleAVAX

	// tx errors:
	ErrWrongNetworkID          = errWrongNetworkID
	ErrWrongChainID            = errWrongChainID
	ErrNoInputs                = errNoInputs
	ErrNoOutputs               = errNoOutputs
	ErrNotSameSubnet           = errNotSameSubnet
	ErrInvalidInput            = errInvalidInput
	ErrNonAVAXInput            = errNonAVAXInput
	ErrInvalidOutput           = errInvalidOutput
	ErrNonAVAXOutput           = errNonAVAXOutput
	ErrFlowCheckFailed         = errFlowCheckFailed
	ErrInputsNotSortedUnique   = errInputsNotSortedUnique
	ErrOverflow                = errOverflow
	ErrIncorrectNumCredentials = errIncorrectNumCredentials
	ErrConvertingToFxTx        = errConvertingToFxTx

	// codec errors:
	ErrInefficientSlicePacking = errInefficientSlicePacking

	// export errors:
	ErrOutputsNotSorted       = errOutputsNotSorted
	ErrMultipleNonces         = errMultipleNonces
	ErrInsufficientFunds      = errInsufficientFunds
	ErrIncorrectNumSignatures = errIncorrectNumSignatures
	ErrRecoveringPublicKey    = errRecoveringPublicKey
	ErrAddressMismatch        = errAddressMismatch

	// import errors:
	ErrOutputsNotSortedUnique = errOutputsNotSortedUnique
	ErrUnexpectedInputType    = errUnexpectedInputType
	ErrFetchingUTXOs          = errFetchingUTXOs
	ErrUnmarshallingUTXO      = errUnmarshallingUTXO
	ErrMismatchedAssetIDs     = errMismatchedAssetIDs
	ErrVerifyingTransfer      = errVerifyingTransfer
)

func MarshalUTXO(tb testing.TB, utxo *avax.UTXO) []byte {
	tb.Helper()

	b, err := c.Marshal(codecVersion, utxo)
	require.NoError(tb, err, "%T.Marshal(%T)", c, utxo)
	return b
}
