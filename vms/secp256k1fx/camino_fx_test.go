// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/require"
)

func TestVerifyPermissionUnordered(t *testing.T) {
	tx := &TestTx{}

	key1, addr1 := generateKey(t)
	key2, addr2 := generateKey(t)

	tests := map[string]struct {
		tx          UnsignedTx
		owner       interface{}
		signers     []*crypto.PrivateKeySECP256K1R
		expectedErr error
	}{
		"OK, 1 addr": {
			tx:      tx,
			signers: []*crypto.PrivateKeySECP256K1R{key1},
			owner: &OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr1},
			},
		},
		"OK, 2 addr": {
			tx:      tx,
			signers: []*crypto.PrivateKeySECP256K1R{key2, key1},
			owner: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
		},
		"Fail: to few sigs": {
			tx:      tx,
			signers: []*crypto.PrivateKeySECP256K1R{key1},
			owner: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			expectedErr: errTooFewSigners,
		},
		"Fail: wrong sig": {
			tx:      tx,
			signers: []*crypto.PrivateKeySECP256K1R{key2},
			owner: &OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr1},
			},
			expectedErr: errTooFewSigners,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			fx := defaultFx(t)

			// generating credential

			txHash := hashing.ComputeHash256(tt.tx.Bytes())
			cred := &Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(tt.signers))}

			for i, key := range tt.signers {
				sig, err := key.SignHash(txHash)
				require.NoError(err)
				copy(cred.Sigs[i][:], sig)
			}

			// testing

			err := fx.VerifyPermissionUnordered(tt.tx, cred, tt.owner)
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}

func defaultFx(t *testing.T) *Fx {
	require := require.New(t)
	vm := TestVM{
		Codec: linearcodec.NewDefault(),
		Log:   logging.NoLog{},
	}
	date := time.Date(2019, time.January, 19, 16, 25, 17, 3, time.UTC)
	vm.Clk.Set(date)
	fx := Fx{}
	require.NoError(fx.Initialize(&vm))
	require.NoError(fx.Bootstrapping())
	require.NoError(fx.Bootstrapped())
	return &fx
}

func generateKey(t *testing.T) (*crypto.PrivateKeySECP256K1R, ids.ShortID) {
	require := require.New(t)
	secpFactory := crypto.FactorySECP256K1R{}
	key, err := secpFactory.NewPrivateKey()
	require.NoError(err)

	secpKey, ok := key.(*crypto.PrivateKeySECP256K1R)
	require.True(ok)

	return secpKey, secpKey.Address()
}
