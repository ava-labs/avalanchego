// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestAggregation(t *testing.T) {
	type test struct {
		name                   string
		setup                  func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte)
		expectedSigAggError    error
		expectedPubKeyAggError error
		expectedValid          bool
	}

	tests := []test{
		{
			name: "valid",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
					PublicFromSecretKey(sk2),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
					Sign(sk2, msg),
				}

				return pks, sigs, msg
			},
			expectedValid: true,
		},
		{
			name: "valid single key",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk, msg),
				}

				return pks, sigs, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
					PublicFromSecretKey(sk2),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
					Sign(sk2, msg),
				}

				msg[0]++

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "one sig over different message",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
					PublicFromSecretKey(sk2),
				}

				msg := utils.RandomBytes(1234)
				msg2 := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
					Sign(sk2, msg2),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "one incorrect pubkey",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)
				sk3, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
					PublicFromSecretKey(sk3),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
					Sign(sk2, msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "num pubkeys > num sigs",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
					PublicFromSecretKey(sk2),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "num pubkeys < num sigs",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
					Sign(sk2, msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name: "no pub keys",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				msg := utils.RandomBytes(1234)

				sigs := []*Signature{
					Sign(sk0, msg),
					Sign(sk1, msg),
					Sign(sk2, msg),
				}

				return nil, sigs, msg
			},
			expectedPubKeyAggError: ErrNoPublicKeys,
			expectedValid:          false,
		},
		{
			name: "no sigs",
			setup: func(require *require.Assertions) ([]*PublicKey, []*Signature, []byte) {
				sk0, err := NewSecretKey()
				require.NoError(err)
				sk1, err := NewSecretKey()
				require.NoError(err)
				sk2, err := NewSecretKey()
				require.NoError(err)

				pks := []*PublicKey{
					PublicFromSecretKey(sk0),
					PublicFromSecretKey(sk1),
					PublicFromSecretKey(sk2),
				}

				msg := utils.RandomBytes(1234)
				return pks, nil, msg
			},
			expectedSigAggError: errNoSignatures,
			expectedValid:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			pks, sigs, msg := tt.setup(require)

			aggSig, err := AggregateSignatures(sigs)
			require.ErrorIs(err, tt.expectedSigAggError)

			aggPK, err := AggregatePublicKeys(pks)
			require.ErrorIs(err, tt.expectedPubKeyAggError)

			valid := Verify(aggPK, aggSig, msg)
			require.Equal(tt.expectedValid, valid)
		})
	}
}

func TestAggregationThreshold(t *testing.T) {
	require := require.New(t)

	// People in the network would privately generate their secret keys
	sk0, err := NewSecretKey()
	require.NoError(err)
	sk1, err := NewSecretKey()
	require.NoError(err)
	sk2, err := NewSecretKey()
	require.NoError(err)

	// All the public keys would be registered on chain
	pks := []*PublicKey{
		PublicFromSecretKey(sk0),
		PublicFromSecretKey(sk1),
		PublicFromSecretKey(sk2),
	}

	// The transaction's unsigned bytes are publicly known.
	msg := utils.RandomBytes(1234)

	// People may attempt time sign the transaction.
	sigs := []*Signature{
		Sign(sk0, msg),
		Sign(sk1, msg),
		Sign(sk2, msg),
	}

	// The signed transaction would specify which of the public keys have been
	// used to sign it. The aggregator should verify each individual signature,
	// until it has found a sufficient threshold of valid signatures.
	var (
		indices      = []int{0, 2}
		filteredPKs  = make([]*PublicKey, len(indices))
		filteredSigs = make([]*Signature, len(indices))
	)
	for i, index := range indices {
		pk := pks[index]
		filteredPKs[i] = pk
		sig := sigs[index]
		filteredSigs[i] = sig

		valid := Verify(pk, sig, msg)
		require.True(valid)
	}

	// Once the aggregator has the required threshold of signatures, it can
	// aggregate the signatures.
	aggregatedSig, err := AggregateSignatures(filteredSigs)
	require.NoError(err)

	// For anyone looking for a proof of the aggregated signature's correctness,
	// they can aggregate the public keys and verify the aggregated signature.
	aggregatedPK, err := AggregatePublicKeys(filteredPKs)
	require.NoError(err)

	valid := Verify(aggregatedPK, aggregatedSig, msg)
	require.True(valid)
}

func TestVerify(t *testing.T) {
	type test struct {
		name          string
		setup         func(*require.Assertions) (pk *PublicKey, sig *Signature, msg []byte)
		expectedValid bool
	}

	tests := []test{
		{
			name: "valid",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk)
				msg := utils.RandomBytes(1234)
				sig := Sign(sk, msg)
				return pk, sig, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk)
				msg := utils.RandomBytes(1234)
				sig := Sign(sk, msg)
				msg[0]++
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong pub key",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				msg := utils.RandomBytes(1234)
				sig := Sign(sk, msg)

				sk2, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk2)
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong sig",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk)
				msg := utils.RandomBytes(1234)

				msg2 := utils.RandomBytes(1234)
				sig2 := Sign(sk, msg2)
				return pk, sig2, msg
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			pk, sig, msg := tt.setup(require)
			valid := Verify(pk, sig, msg)
			require.Equal(tt.expectedValid, valid)
			valid = VerifyProofOfPossession(pk, sig, msg)
			require.False(valid)
		})
	}
}

func TestVerifyProofOfPossession(t *testing.T) {
	type test struct {
		name          string
		setup         func(*require.Assertions) (pk *PublicKey, sig *Signature, msg []byte)
		expectedValid bool
	}

	tests := []test{
		{
			name: "valid",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk)
				msg := utils.RandomBytes(1234)
				sig := SignProofOfPossession(sk, msg)
				return pk, sig, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk)
				msg := utils.RandomBytes(1234)
				sig := SignProofOfPossession(sk, msg)
				msg[0]++
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong pub key",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				msg := utils.RandomBytes(1234)
				sig := SignProofOfPossession(sk, msg)

				sk2, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk2)
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong sig",
			setup: func(require *require.Assertions) (*PublicKey, *Signature, []byte) {
				sk, err := NewSecretKey()
				require.NoError(err)
				pk := PublicFromSecretKey(sk)
				msg := utils.RandomBytes(1234)

				msg2 := utils.RandomBytes(1234)
				sig2 := SignProofOfPossession(sk, msg2)
				return pk, sig2, msg
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			pk, sig, msg := tt.setup(require)
			valid := VerifyProofOfPossession(pk, sig, msg)
			require.Equal(tt.expectedValid, valid)
			valid = Verify(pk, sig, msg)
			require.False(valid)
		})
	}
}
