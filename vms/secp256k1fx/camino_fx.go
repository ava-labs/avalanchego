// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var errNotSecp256Cred = errors.New("expected secp256k1 credentials")

func (fx *Fx) RecoverAddresses(utx UnsignedTx, verifies []verify.Verifiable) (set.Set[ids.ShortID], error) {
	var ret set.Set[ids.ShortID]
	visited := make(map[[crypto.SECP256K1RSigLen]byte]bool)

	txHash := hashing.ComputeHash256(utx.Bytes())
	for _, v := range verifies {
		cred, ok := v.(*Credential)
		if !ok {
			return nil, errNotSecp256Cred
		}
		for _, sig := range cred.Sigs {
			if visited[sig] {
				continue
			}
			pk, err := fx.SECPFactory.RecoverHashPublicKey(txHash, sig[:])
			if err != nil {
				return nil, err
			}
			visited[sig] = true
			ret.Add(pk.Address())
		}
	}
	return ret, nil
}
