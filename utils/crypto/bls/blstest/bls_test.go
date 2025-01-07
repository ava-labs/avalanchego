// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls_test

import (
	"io"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signers/localsigner"

	jsonrpc "github.com/ava-labs/avalanchego/utils/crypto/bls/signers/json-rpc"
)

type Signer interface {
	bls.Signer
	io.Closer
}

type jsonRPCSigner struct {
	*jsonrpc.Client
	server *jsonrpc.Server
}

func (s jsonRPCSigner) Close() error {
	return s.server.Close()
}

type localSigner struct {
	*localsigner.LocalSigner
}

func (s localSigner) Close() error {
	return nil
}

var localSignerFn = func() (Signer, error) {
	signer, err := localsigner.NewSigner()
	return &localSigner{LocalSigner: signer}, err
}

var serverSignerFn = func() (Signer, error) {
	// do I need to make sure the server gets closed properly?
	service := jsonrpc.NewSignerService()
	server, err := jsonrpc.Serve(service)
	if err != nil {
		return nil, err
	}

	url := url.URL{
		Scheme: "http",
		Host:   server.Addr().String(),
	}

	client := jsonrpc.NewClient(url)

	return jsonRPCSigner{server: server, Client: client}, nil
}

var signerFns = []func() (Signer, error){
	localSignerFn,
	serverSignerFn,
}

func TestAggregation(t *testing.T) {
	type test struct {
		name                   string
		signers                []func() (Signer, error)
		setup                  func(*require.Assertions, bls.Signer) (pks []*bls.PublicKey, sigs []*bls.Signature, msg []byte)
		expectedSigAggError    error
		expectedPubKeyAggError error
		expectedValid          bool
	}

	tests := []test{
		{
			name:    "valid",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: true,
		},
		{
			name:    "valid single key",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				pks := []*bls.PublicKey{
					signer.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				msg[0]++

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name:    "one sig over different message",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)
				msg2 := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg2),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name:    "one incorrect pubkey",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)
				sk3, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
					sk1.PublicKey(),
					sk3.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name:    "num pubkeys > num sigs",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
					sk1.PublicKey(),
					sk2.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name:    "num pubkeys < num sigs",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
					sk1.PublicKey(),
				}

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return pks, sigs, msg
			},
			expectedValid: false,
		},
		{
			name:    "no pub keys",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				msg := utils.RandomBytes(1234)

				sigs := []*bls.Signature{
					signer.Sign(msg),
					sk1.Sign(msg),
					sk2.Sign(msg),
				}

				return nil, sigs, msg
			},
			expectedPubKeyAggError: bls.ErrNoPublicKeys,
			expectedValid:          false,
		},
		{
			name:    "no sigs",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) ([]*bls.PublicKey, []*bls.Signature, []byte) {
				sk1, err := localsigner.NewSigner()
				require.NoError(err)
				sk2, err := localsigner.NewSigner()
				require.NoError(err)

				pks := []*bls.PublicKey{
					signer.PublicKey(),
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
			for _, newSigner := range tt.signers {
				require := require.New(t)

				signer, err := newSigner()
				require.NoError(err)

				pks, sigs, msg := tt.setup(require, signer)

				aggSig, err := bls.AggregateSignatures(sigs)
				require.ErrorIs(err, tt.expectedSigAggError)

				aggPK, err := bls.AggregatePublicKeys(pks)
				require.ErrorIs(err, tt.expectedPubKeyAggError)

				valid := bls.Verify(aggPK, aggSig, msg)
				require.Equal(tt.expectedValid, valid)
				signer.Close()
			}
		})
	}
}

func TestAggregationThreshold(t *testing.T) {
	require := require.New(t)

	// People in the network would privately generate their secret keys
	sk0, err := localSignerFn()
	require.NoError(err)
	sk1, err := serverSignerFn()
	require.NoError(err)
	defer sk1.Close()

	require.NoError(err)
	sk2, err := localsigner.NewSigner()
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
		signers       []func() (Signer, error)
		setup         func(*require.Assertions, bls.Signer) (pk *bls.PublicKey, sig *bls.Signature, msg []byte)
		expectedValid bool
	}

	tests := []test{
		{
			name: "valid",
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := signer.Sign(msg)
				return pk, sig, msg
			},
			expectedValid: true,
		},
		{
			name: "wrong message",
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := signer.Sign(msg)
				msg[0]++
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong pub key",
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				msg := utils.RandomBytes(1234)
				sig := signer.Sign(msg)

				sk2, err := localsigner.NewSigner()
				require.NoError(err)
				pk := sk2.PublicKey()
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name: "wrong sig",
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)

				msg2 := utils.RandomBytes(1234)
				sig2 := signer.Sign(msg2)
				return pk, sig2, msg
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, newSigner := range tt.signers {
				require := require.New(t)
				signer, err := newSigner()
				require.NoError(err)
				pk, sig, msg := tt.setup(require, signer)
				valid := bls.Verify(pk, sig, msg)
				require.Equal(tt.expectedValid, valid)
				valid = bls.VerifyProofOfPossession(pk, sig, msg)
				require.False(valid)
				signer.Close()
			}
		})
	}
}

func TestVerifyProofOfPossession(t *testing.T) {
	type test struct {
		name          string
		signers       []func() (Signer, error)
		setup         func(*require.Assertions, bls.Signer) (pk *bls.PublicKey, sig *bls.Signature, msg []byte)
		expectedValid bool
	}

	tests := []test{
		{
			name:    "valid",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				pk := signer.PublicKey()
				msg := utils.RandomBytes(1234)
				sig := signer.SignProofOfPossession(msg)
				return pk, sig, msg
			},
			expectedValid: true,
		},
		{
			name:    "wrong message",
			signers: signerFns,
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
			name:    "wrong pub key",
			signers: signerFns,
			setup: func(require *require.Assertions, signer bls.Signer) (*bls.PublicKey, *bls.Signature, []byte) {
				msg := utils.RandomBytes(1234)
				sig := signer.SignProofOfPossession(msg)

				sk2, err := localsigner.NewSigner()
				require.NoError(err)
				pk := sk2.PublicKey()
				return pk, sig, msg
			},
			expectedValid: false,
		},
		{
			name:    "wrong sig",
			signers: signerFns,
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
				signer.Close()
			}
		})
	}
}
