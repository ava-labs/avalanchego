// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
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
		signers       []*secp256k1.PrivateKey
		owners        *OutputOwners
		msig          func(c *gomock.Controller) AliasGetter
		expectedError error
	}{
		"OK: 2 addrs, theshold 2": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*secp256k1.PrivateKey{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig: noAliasesMsigGetter,
		},
		"Fail: To few signature indices": {
			in:      &Input{SigIndices: []uint32{0}},
			signers: []*secp256k1.PrivateKey{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: ErrInputCredentialSignersMismatch,
		},
		"Fail: To many signature indices": {
			in:      &Input{SigIndices: []uint32{0, 1, 2}},
			signers: []*secp256k1.PrivateKey{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: ErrInputCredentialSignersMismatch,
		},
		"Fail: To many signers": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*secp256k1.PrivateKey{key1, key2, key3},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: ErrInputCredentialSignersMismatch,
		},
		"Fail: Not enough sigs to reach treshold": {
			in:      &Input{SigIndices: []uint32{0}},
			signers: []*secp256k1.PrivateKey{key1},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"Fail: Wrong signature": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*secp256k1.PrivateKey{key1, key1},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"Fail: Signature index points to wrong signature": {
			in:      &Input{SigIndices: []uint32{0, 0}},
			signers: []*secp256k1.PrivateKey{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig:          noAliasesMsigGetter,
			expectedError: errCantSpend,
		},
		"OK msig: addr1, alias1{addr2, addr3, thresh: 1}": {
			in:      &Input{SigIndices: []uint32{0, 1}},
			signers: []*secp256k1.PrivateKey{key1, key2},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.AliasWithNonce{{Alias: multisig.Alias{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}}})
				return msig
			},
		},
		"OK msig: addr1, alias1{addr2, addr3, thresh: 2}": {
			in:      &Input{SigIndices: []uint32{0, 1, 2}},
			signers: []*secp256k1.PrivateKey{key1, key2, key3},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.AliasWithNonce{{Alias: multisig.Alias{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}}})
				return msig
			},
		},
		"OK msig: alias1{ alias2{addr1, addr2, thresh: 2}, addr3, thresh: 2 }, addr4": {
			in:      &Input{SigIndices: []uint32{0, 1, 2, 3}},
			signers: []*secp256k1.PrivateKey{key1, key2, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.AliasWithNonce{
					{Alias: multisig.Alias{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					}},
					{Alias: multisig.Alias{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					}},
				})
				return msig
			},
		},
		"Fail msig: alias1{ alias2{addr1, addr2 (bad sig), thresh: 2}, addr3, thresh: 2 }, addr4": {
			in:      &Input{SigIndices: []uint32{0, 1, 2, 3}},
			signers: []*secp256k1.PrivateKey{key1, key4, key3, key4},
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{aliasAddr1, addr4},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.AliasWithNonce{
					{Alias: multisig.Alias{
						ID: aliasAddr1,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{aliasAddr2, addr3},
						},
					}},
					{Alias: multisig.Alias{
						ID: aliasAddr2,
						Owners: &OutputOwners{
							Threshold: 2,
							Addrs:     []ids.ShortID{addr1, addr2},
						},
					}},
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

			cred := &Credential{Sigs: make([][secp256k1.SignatureLen]byte, len(tt.signers))}
			for i, key := range tt.signers {
				sig, err := key.SignHash(txHash)
				require.NoError(t, err)
				copy(cred.Sigs[i][:], sig)
			}

			err := fx.verifyMultisigCredentials(tx.Bytes(), tt.in, cred, tt.owners, tt.msig(ctrl))
			require.ErrorIs(t, err, tt.expectedError)
		})
	}
}

func TestExtractFromAndSigners(t *testing.T) {
	key1, addr1 := generateKey(t)
	key2, addr2 := generateKey(t)
	key3, addr3 := generateKey(t)
	key4, addr4 := generateKey(t)

	tests := map[string]struct {
		signers        []*secp256k1.PrivateKey
		expectedFrom   set.Set[ids.ShortID]
		expectedSigner []*secp256k1.PrivateKey
		expectedError  error
	}{
		"OK: Split 4 + 0": {
			signers:        []*secp256k1.PrivateKey{key1, key2, key3, key4},
			expectedFrom:   makeShortIDSet([]ids.ShortID{addr1, addr2, addr3, addr4}),
			expectedSigner: []*secp256k1.PrivateKey{key1, key2, key3, key4},
		},
		"OK: Split 2 + 2": {
			signers:        []*secp256k1.PrivateKey{key1, key2, nil, key3, key4},
			expectedFrom:   makeShortIDSet([]ids.ShortID{addr1, addr2}),
			expectedSigner: []*secp256k1.PrivateKey{key3, key4},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f, s := ExtractFromAndSigners(tt.signers)
			require.Equal(t, f, tt.expectedFrom)
			require.Equal(t, s, tt.expectedSigner)
		})
	}
}

func TestCollectMultisigAliases(t *testing.T) {
	_, addr1 := generateKey(t)
	_, addr2 := generateKey(t)
	_, addr3 := generateKey(t)
	_, aliasAddr1 := generateKey(t)

	noAliasesMsigGetter := func(c *gomock.Controller) AliasGetter {
		msig := NewMockAliasGetter(c)
		msig.EXPECT().GetMultisigAlias(gomock.Any()).Return(nil, database.ErrNotFound).AnyTimes()
		return msig
	}

	tests := map[string]struct {
		owners          *OutputOwners
		msig            func(c *gomock.Controller) AliasGetter
		expectedAliases int
		expectedError   error
	}{
		"OK: 2 addrs, no msig alias": {
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, addr2},
			},
			msig: noAliasesMsigGetter,
		},
		"OK 1 msig alias": {
			owners: &OutputOwners{
				Threshold: 2,
				Addrs:     []ids.ShortID{addr1, aliasAddr1},
			},
			msig: func(c *gomock.Controller) AliasGetter {
				msig := NewMockAliasGetter(c)
				expectGetMultisigAliases(msig, []*multisig.AliasWithNonce{{Alias: multisig.Alias{
					ID: aliasAddr1,
					Owners: &OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr2, addr3},
					},
				}}})
				return msig
			},
			expectedAliases: 1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			aliases, err := collectMultisigAliases(tt.owners, tt.msig(ctrl))
			require.ErrorIs(t, err, tt.expectedError)
			require.Equal(t, len(aliases), tt.expectedAliases)
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

func generateKey(t *testing.T) (*secp256k1.PrivateKey, ids.ShortID) {
	require := require.New(t)
	secpFactory := secp256k1.Factory{}
	key, err := secpFactory.NewPrivateKey()
	require.NoError(err)
	return key, key.Address()
}

func expectGetMultisigAliases(msig *MockAliasGetter, aliases []*multisig.AliasWithNonce) {
	aliasesMatchers := []gomock.Matcher{}
	for _, alias := range aliases {
		aliasesMatchers = append(aliasesMatchers, gomock.Eq(alias.ID))
		msig.EXPECT().GetMultisigAlias(alias.ID).Return(alias, nil)
	}
	msig.EXPECT().GetMultisigAlias(gomock.Not(gomock.All(aliasesMatchers...))).
		Return(nil, database.ErrNotFound).AnyTimes()
}

func makeShortIDSet(src []ids.ShortID) set.Set[ids.ShortID] {
	newSet := set.NewSet[ids.ShortID](len(src))
	newSet.Add(src...)
	return newSet
}
