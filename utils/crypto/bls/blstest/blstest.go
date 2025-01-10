// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blstest

import (
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func AggregateAndVerify(publicKeys []*bls.PublicKey, signatures []*bls.Signature, message []byte) (bool, error) {
	aggSig, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return false, err
	}
	aggPK, err := bls.AggregatePublicKeys(publicKeys)
	if err != nil {
		return false, err
	}

	return bls.Verify(aggPK, aggSig, message), nil
}
