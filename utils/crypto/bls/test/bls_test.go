// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signers/local"
)

func TestAggregation(t *testing.T) {
	type test struct {
		name                   string
		setup                  func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte)
		expectedSigAggError    error
		expectedPubKeyAggError error
		expectedValid          bool
	}

	tests := []test{
		{
			name: "valid",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: true,
		},
		{
			name: "valid single key",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				msg[0]++

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "one sig over different message",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)
				msg2 := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg2),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "one incorrect pubkey",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)
				sk3, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
					sk3.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "num pubkeys > num sigs",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "num pubkeys < num sigs",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "no pub keys",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					sk0.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return nil, sigs, msg
			},
			expectedPubKeyAggError: bls.ErrNoPublicKeys,
			expectedValid:          false,
		},
		{
			name: "no sigs",
			setup: func(require *require.Assertions) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk0, err := local.NewSigner()
				require.NoError(err)
				sk1, err := local.NewSigner()
				require.NoError(err)
				sk2, err := local.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					sk0.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)
				return pks, nil, msg
			},
			expectedSigAggError: bls.ErrNoSignatures,
			expectedValid:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			pks, sigs, msg := tt.setup(require)

			aggSig, err := bls.AggregateSignatures(sigs)
			require.ErrorIs(err, tt.expectedSigAggError)

			aggPK, err := bls.AggregatePublicKeys(pks)
			require.ErrorIs(err, tt.expectedPubKeyAggError)

			valid := bls.Verify(aggPK, aggSig, msg)
			require.Equal(tt.expectedValid, valid)
		})
	}
}

func TestAggregationThreshold(t *testing.T) {
	require := require.New(t)

	// People in the network would privately generate their secret keys
	sk0, err := local.NewSigner()
	require.NoError(err)
	sk1, err := local.NewSigner()
	require.NoError(err)
	sk2, err := local.NewSigner()
	require.NoError(err)

	// All the public keys would be registered on chain
	pks := []*bls.PublicKey{
		sk0.PublicKey(),
		sk1.PublicKey(),
		sk2.PublicKey(),
	}

	// The transaction's unsigned bytes are publicly known.
	msg := utils.RandomBytes(1234)

	// People may attempt time sign the transaction.
	sigs := []*bls.Signature{
		sk0.Sign(msg),
		sk1.Sign(msg),
		sk2.Sign(msg),
	}

	// The signed transaction would specify which of the public keys have been
	// used to sign it. The aggregator should verify each individual signature,
	// until it has found a sufficient threshold of valid signatures.
	var (
		indices      = []int{0, 2}
		filteredPKs  = make([]*bls.PublicKey, len(indices))
		filteredSigs = make([]*bls.Signature, len(indices))
	)
	for i, index := range indices {
		pk := pks[index]
		filteredPKs[i] = pk
		sig := sigs[index]
		filteredSigs[i] = sig

		valid := bls.Verify(pk, sig, msg)
		require.True(valid)
	}

	// Once the aggregator has the required threshold of signatures, it can
	// aggregate the signatures.
	aggregatedSig, err := bls.AggregateSignatures(filteredSigs)
	require.NoError(err)

	// For anyone looking for a proof of the aggregated signature's correctness,
	// they can aggregate the public keys and verify the aggregated signature.
	aggregatedPK, err := bls.AggregatePublicKeys(filteredPKs)
	require.NoError(err)

	valid := bls.Verify(aggregatedPK, aggregatedSig, msg)
	require.True(valid)
}

func TestVerify(t *testing.T) {
	type test struct {
		name          string
		setup         func(*require.Assertions) (pk *bls.PublicKey, sig *bls.Signature, msg []byte)
		expectedValid bool
	}

	tests := []test{
		{
			name: "valid",
			setup: func(require *require.Assertions) (*bls.PublicKey, *bls.Signature, []byte) {
				sk, err := local.NewSigner()
				require.NoError(err)
				pk := sk.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := sk.Sign(msg)
				return pk, sig, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions) (*bls.PublicKey, *bls.Signature, []byte) {
				sk, err := local.NewSigner()
				require.NoError(err)
				pk := sk.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := sk.Sign(msg)
				msg[0]++
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong pub key",
			setup: func(require *require.Assertions) (*bls.PublicKey, *bls.Signature, []byte) {
				sk, err := local.NewSigner()
				require.NoError(err)
				msg := utils.RandomBytes(1234)
				sig := sk.Sign(msg)

				sk2, err := local.NewSigner()
				require.NoError(err)
				pk := sk2.PublicKey()
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong sig",
			setup: func(require *require.Assertions) (*bls.PublicKey, *bls.Signature, []byte) {
				sk, err := local.NewSigner()
				require.NoError(err)
				pk := sk.PublicKey()
				msg := utils.RandomBytes(1234)

				msg2 := utils.RandomBytes(1234)
				sig2 := sk.Sign(msg2)
				return pk, sig2, msg
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			pk, sig, msg := tt.setup(require)
			valid := bls.Verify(pk, sig, msg)
			require.Equal(tt.expectedValid, valid)
			valid = bls.VerifyProofOfPossession(pk, sig, msg)
			require.False(valid)
		})
	}
}

func TestVerifyProofOfPossession(t *testing.T) {
	type test struct {
		name          string
		signers       []func() (bls.Signer, error)
		setup         func(*require.Assertions, bls.Signer) (pk *bls.PublicKey, sig *bls.Signature, msg []byte)
		expectedValid bool
	}

	tests := []test{
		{
			name: "valid",
			signers: []func() (bls.Signer, error){
				func() (bls.Signer, error) { return local.NewSigner() },
			},
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := signer.SignProofOfPossession(msg)
				return pk, sig, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			signers: []func() (bls.Signer, error){
				func() (bls.Signer, error) { return local.NewSigner() },
			},
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := signer.SignProofOfPossession(msg)
				msg[0]++
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong pub key",
			signers: []func() (bls.Signer, error){
				func() (bls.Signer, error) { return local.NewSigner() },
			},
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				msg := utils.RandomBytes(1234)
				sig := signer.SignProofOfPossession(msg)

				sk2, err := local.NewSigner()
				require.NoError(err)
				pk := sk2.PublicKey()
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong sig",
			signers: []func() (bls.Signer, error){
				func() (bls.Signer, error) { return local.NewSigner() },
			},
			setup: func(_ *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)

				msg2 := utils.RandomBytes(1234)
				sig2 := signer.SignProofOfPossession(msg2)
				return pk, sig2, msg
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			for _, newSigner := range tt.signers {
				signer, err := newSigner()
				require.NoError(err)
				pk, sig, msg := tt.setup(require, signer)
				valid := bls.VerifyProofOfPossession(pk, sig, msg)
				require.Equal(tt.expectedValid, valid)
				valid = bls.Verify(pk, sig, msg)
				require.False(valid)
			}
		})
	}
}
