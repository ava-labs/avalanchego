// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestVerifyMultisigCredentials(t *testing.T) {
	key1, addr1 := generateKey(t)
	key2, addr2 := generateKey(t)
	key3, addr3 := generateKey(t)
	key4, addr4 := generateKey(t)
	_, aliasAddr1 := generateKey(t)
	_, aliasAddr2 := generateKey(t)
	tx := &TestTx{}
	txHash := hashing.ComputeHash256(tx.Bytes())

	noAliasesMsigGetter := func(c *gomock.Controller) AliasGetter {
		msig := NewMockAliasGetter(c)
		msig.EXPECT().GetMultisigAlias(gomock.Any()).Return(nil, database.ErrNotFound).AnyTimes()
		return msig
	}

	tests := map[string]struct {
		in            *Input
		signers       []*crypto.PrivateKeySECP256K1R
		owners        *OutputOwners
		msig          func(c *gomock.Controller) AliasGetter
		expectedError error
	}{
		"OK: 2 addrs, theshold 2": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig: noAliasesMsigGetter,
		},
		"Fail: To few signature indices": {
			in:      &Input{SigIndices: []uint32{0}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errTooFewSigners,
		},
		"Fail: To many signature indices": {
			in:      &Input{SigIndices: []uint32{0, 1, 2}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errTooManySigners,
		},
		"Fail: To many signers": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errTooFewSigners,
		},
		"Fail: Not enough sigs to reach treshold": {
			in:      &Input{SigIndices: []uint32{0}},
			signers: []*crypto.PrivateKeySECP256K1R{key1},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errInputOutputIndexOutOfBounds,
		},
		"Fail: Wrong signature": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key1},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"Fail: Signature index points to wrong signature": {
			in:      &Input{SigIndices: []uint32{0, 0}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"OK msig: addr1, alias1{addr2, addr3, thresh: 1}": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}})
				return msig
			},
		},
		"OK msig: addr1, alias1{addr2, addr3, thresh: 2}": {
			in:      &Input{SigIndices: []uint32{0, 1, 2}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}})
				return msig
			},
		},
		"OK msig: alias1{ alias2{addr1, addr2, thresh: 2}, addr3, thresh: 2 }, addr4": {
			in:      &Input{SigIndices: []uint32{0, 1, 2, 3}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
		},
		"OK msig: alias1{ alias2{addr1, addr2 (wildcard), thresh: 2}, addr3, thresh: 2 }, addr4": {
			in:      &Input{SigIndices: []uint32{0, math.MaxUint32, 2, 3}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
		},
		"OK msig: alias1{ alias2{addr1, addr2, thresh: 2}, addr3, thresh: 2 }, addr4, all wildcards": {
			in:      &Input{SigIndices: []uint32{math.MaxUint32, math.MaxUint32, math.MaxUint32, math.MaxUint32}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
		},
		"Fail msig: alias1{ alias2{addr1, addr2 (bad sig), thresh: 2}, addr3, thresh: 2 }, addr4": {
			in:      &Input{SigIndices: []uint32{0, 1, 2, 3}},
			signers: []*crypto.PrivateKeySECP256K1R{key1, key4, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
			expectedError: errCantSpend,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fx := defaultFx(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			cred := &Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(tt.signers))}
			for i, key := range tt.signers {
				sig, err := key.SignHash(txHash)
				require.NoError(t, err)
				copy(cred.Sigs[i][:], sig)
			}

			err := fx.verifyMultisigCredentials(tx, tt.in, cred, tt.owners, tt.msig(ctrl))
			require.ErrorIs(t, err, tt.expectedError)
		})
	}
}

func TestVerifyMultisigUnorderedCredentials(t *testing.T) {
	key1, addr1 := generateKey(t)
	key2, addr2 := generateKey(t)
	key3, addr3 := generateKey(t)
	key4, addr4 := generateKey(t)
	key5, _ := generateKey(t)
	_, aliasAddr1 := generateKey(t)
	_, aliasAddr2 := generateKey(t)
	tx := &TestTx{}
	txHash := hashing.ComputeHash256(tx.Bytes())

	noAliasesMsigGetter := func(c *gomock.Controller) AliasGetter {
		msig := NewMockAliasGetter(c)
		msig.EXPECT().GetMultisigAlias(gomock.Any()).Return(nil, database.ErrNotFound).AnyTimes()
		return msig
	}

	tests := map[string]struct {
		signers       []*crypto.PrivateKeySECP256K1R
		owners        *OutputOwners
		msig          func(c *gomock.Controller) AliasGetter
		expectedError error
	}{
		"OK: 2 addrs, theshold 2": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig: noAliasesMsigGetter,
		},
		"OK: more signers, than needed": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig: noAliasesMsigGetter,
		},
		"Not enough sigs to reach treshold": {
			signers: []*crypto.PrivateKeySECP256K1R{key1},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"Wrong signature": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key1},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"OK msig: addr1, alias1{addr2, addr3, thresh: 1}": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}})
				return msig
			},
		},
		"OK msig: addr1, alias1{addr2, addr3, thresh: 2}": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}})
				return msig
			},
		},
		"OK msig: alias1{ alias2{addr1, addr2, thresh: 2}, addr3, thresh: 2 }, addr4": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key2, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
		},
		"OK msig: more signers, than needed alias1{ alias2{addr1, addr2, thresh: 1}, addr3, thresh: 1 }, addr4": {
			signers: []*crypto.PrivateKeySECP256K1R{key2, key4, key5},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
		},
		"Fail msig: alias1{ alias2{addr1, addr2 (bad sig), thresh: 2}, addr3, thresh: 2 }, addr4": {
			signers: []*crypto.PrivateKeySECP256K1R{key1, key4, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.Alias{
					{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					},
					{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					},
				})
				return msig
			},
			expectedError: errCantSpend,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fx := defaultFx(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			cred := &Credential{Sigs: make([][crypto.SECP256K1RSigLen]byte, len(tt.signers))}
			for i, key := range tt.signers {
				sig, err := key.SignHash(txHash)
				require.NoError(t, err)
				copy(cred.Sigs[i][:], sig)
			}

			err := fx.verifyMultisigUnorderedCredentials(tx, cred, tt.owners, tt.msig(ctrl))
			require.ErrorIs(t, err, tt.expectedError)
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

func expectGetMultisigAliases(msig *MockAliasGetter, aliases []*multisig.Alias) {
	aliasesMatchers := []gomock.Matcher{}
	for _, alias := range aliases {
		aliasesMatchers = append(aliasesMatchers, gomock.Eq(alias.ID))
		msig.EXPECT().GetMultisigAlias(alias.ID).Return(alias, nil)
	}
	msig.EXPECT().GetMultisigAlias(gomock.Not(gomock.All(aliasesMatchers...))).
		Return(nil, database.ErrNotFound).AnyTimes()
}
