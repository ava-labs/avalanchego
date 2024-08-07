// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func generateOwnersAndSig(t *testing.T, key *secp256k1.PrivateKey, tx txs.UnsignedTx) (secp256k1fx.OutputOwners, *secp256k1fx.Credential) {
	t.Helper()

	txHash := hashing.ComputeHash256(tx.Bytes())
	sig, err := key.SignHash(txHash)
	require.NoError(t, err)

	cred := &secp256k1fx.Credential{Sigs: make([][secp256k1.SignatureLen]byte, 1)}
	copy(cred.Sigs[0][:], sig)

	return secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{key.Address()},
	}, cred
}

func defaultCaminoHandler(t *testing.T) *caminoHandler {
	t.Helper()

	clk := test.Clock()
	vm := &secp256k1fx.TestVM{
		Clk:   *clk,
		Log:   logging.NoLog{},
		Codec: linearcodec.NewDefault(),
	}
	fx := &secp256k1fx.Fx{}
	require.NoError(t, fx.InitializeVM(vm))
	require.NoError(t, fx.Bootstrapped())

	return &caminoHandler{
		handler: handler{
			ctx: snow.DefaultContextTest(),
			clk: clk,
			fx:  fx,
		},
		lockModeBondDeposit: true,
	}
}
