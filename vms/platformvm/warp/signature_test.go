// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
)

const pChainHeight uint64 = 1337

var (
	_ utils.Sortable[*testValidator] = (*testValidator)(nil)

	errTest       = errors.New("non-nil error")
	sourceChainID = ids.GenerateTestID()
	subnetID      = ids.GenerateTestID()

	testVdrs []*testValidator
)

type testValidator struct {
	nodeID ids.NodeID
	sk     bls.Signer
	vdr    *validators.Warp
}

func (v *testValidator) Compare(o *testValidator) int {
	return v.vdr.Compare(o.vdr)
}

func newTestValidator() *testValidator {
	sk, err := localsigner.New()
	if err != nil {
		panic(err)
	}

	nodeID := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	return &testValidator{
		nodeID: nodeID,
		sk:     sk,
		vdr: &validators.Warp{
			PublicKey:      pk,
			PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
			Weight:         3,
			NodeIDs:        []ids.NodeID{nodeID},
		},
	}
}

func init() {
	testVdrs = []*testValidator{
		newTestValidator(),
		newTestValidator(),
		newTestValidator(),
	}
	utils.Sort(testVdrs)
}

func TestNumSigners(t *testing.T) {
	tests := map[string]struct {
		generateSignature func() *BitSetSignature
		count             int
		err               error
	}{
		"empty signers": {
			generateSignature: func() *BitSetSignature {
				return &BitSetSignature{}
			},
		},
		"invalid signers": {
			generateSignature: func() *BitSetSignature {
				return &BitSetSignature{
					Signers: make([]byte, 1),
				}
			},
			err: ErrInvalidBitSet,
		},
		"no signers": {
			generateSignature: func() *BitSetSignature {
				signers := set.NewBits()
				return &BitSetSignature{
					Signers: signers.Bytes(),
				}
			},
		},
		"1 signer": {
			generateSignature: func() *BitSetSignature {
				signers := set.NewBits()
				signers.Add(2)
				return &BitSetSignature{
					Signers: signers.Bytes(),
				}
			},
			count: 1,
		},
		"multiple signers": {
			generateSignature: func() *BitSetSignature {
				signers := set.NewBits()
				signers.Add(2)
				signers.Add(11)
				signers.Add(55)
				signers.Add(93)
				return &BitSetSignature{
					Signers: signers.Bytes(),
				}
			},
			count: 4,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			sig := tt.generateSignature()
			count, err := sig.NumSigners()
			require.Equal(tt.count, count)
			require.ErrorIs(err, tt.err)
		})
	}
}

func TestSignatureVerification(t *testing.T) {
	unsignedMsg, err := NewUnsignedMessage(
		constants.UnitTestID,
		sourceChainID,
		nil,
	)
	require.NoError(t, err)
	unsignedBytes := unsignedMsg.Bytes()

	sign := func(signers ...bls.Signer) [bls.SignatureLen]byte {
		sigs := make([]*bls.Signature, 0, len(signers))
		for _, vdr := range signers {
			sig, err := vdr.Sign(unsignedBytes)
			require.NoError(t, err)
			sigs = append(sigs, sig)
		}
		aggSig, err := bls.AggregateSignatures(sigs)
		require.NoError(t, err)
		aggSigBytes := [bls.SignatureLen]byte{}
		copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))
		return aggSigBytes
	}

	const quorumNum = 1
	tests := []struct {
		name       string
		networkID  uint32
		validators validators.WarpSet
		quorumDen  uint64
		signature  *BitSetSignature
		wantErr    error
	}{
		{
			name:      "wrong_networkID",
			networkID: constants.UnitTestID + 1,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
					testVdrs[2].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 2,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0, 1).Bytes(),
				Signature: sign(testVdrs[0].sk, testVdrs[2].sk),
			},
			wantErr: ErrWrongNetworkID,
		},
		{
			name:      "inefficient_bitset",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 10000,
			signature: &BitSetSignature{
				Signers:   []byte{0x00, 0x01}, // padded byte
				Signature: sign(testVdrs[0].sk),
			},
			wantErr: ErrInvalidBitSet,
		},
		{
			name:      "unknown_index",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 10000,
			signature: &BitSetSignature{
				Signers:   set.NewBits(1).Bytes(),
				Signature: sign(testVdrs[0].sk),
			},
			wantErr: ErrUnknownValidator,
		},
		{
			name:      "insufficient_weight",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
					testVdrs[1].vdr,
					testVdrs[2].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 2,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0).Bytes(),
				Signature: sign(testVdrs[0].sk),
			},
			wantErr: ErrInsufficientWeight,
		},
		{
			name:      "impossible_to_have_sufficient_weight",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 2,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0).Bytes(),
				Signature: sign(testVdrs[0].sk),
			},
			wantErr: ErrInsufficientWeight,
		},
		{
			name:      "malformed_signature",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 10000,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0).Bytes(),
				Signature: [bls.SignatureLen]byte{},
			},
			wantErr: ErrParseSignature,
		},
		{
			name:      "invalid_signature",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 10000,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0).Bytes(),
				Signature: sign(testVdrs[1].sk),
			},
			wantErr: ErrInvalidSignature,
		},
		{
			name:      "valid",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
					testVdrs[2].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 2,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0, 1).Bytes(),
				Signature: sign(testVdrs[0].sk, testVdrs[2].sk),
			},
		},
		{
			name:      "valid_partial_signers",
			networkID: constants.UnitTestID,
			validators: validators.WarpSet{
				Validators: []*validators.Warp{
					testVdrs[0].vdr,
					testVdrs[1].vdr,
					testVdrs[2].vdr,
				},
				TotalWeight: testVdrs[0].vdr.Weight +
					testVdrs[1].vdr.Weight +
					testVdrs[2].vdr.Weight,
			},
			quorumDen: 2,
			signature: &BitSetSignature{
				Signers:   set.NewBits(0, 2).Bytes(),
				Signature: sign(testVdrs[0].sk, testVdrs[2].sk),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err = tt.signature.Verify(
				unsignedMsg,
				tt.networkID,
				tt.validators,
				quorumNum,
				tt.quorumDen,
			)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func BenchmarkSignatureVerification(b *testing.B) {
	unsignedMsg, err := NewUnsignedMessage(
		constants.UnitTestID,
		sourceChainID,
		[]byte{1, 2, 3},
	)
	require.NoError(b, err)
	unsignedBytes := unsignedMsg.Bytes()

	for size := 1; size <= 1<<10; size *= 2 {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			vdrs := make(
				map[ids.NodeID]*validators.GetValidatorOutput,
				size,
			)
			signers := set.NewBits()
			var signatures []*bls.Signature
			for i := range size {
				nodeID := ids.GenerateTestNodeID()
				secretKey, err := localsigner.New()
				require.NoError(b, err)
				publicKey := secretKey.PublicKey()
				vdrs[nodeID] = &validators.GetValidatorOutput{
					NodeID:    nodeID,
					PublicKey: publicKey,
					Weight:    1,
				}

				sig, err := secretKey.Sign(unsignedBytes)
				require.NoError(b, err)

				signers.Add(i)
				signatures = append(signatures, sig)
			}

			aggSig, err := bls.AggregateSignatures(signatures)
			require.NoError(b, err)
			aggSigBytes := [bls.SignatureLen]byte{}
			copy(aggSigBytes[:], bls.SignatureToBytes(aggSig))

			canonicalValidators, err := validators.FlattenValidatorSet(vdrs)
			require.NoError(b, err)

			sig := &BitSetSignature{
				Signers:   signers.Bytes(),
				Signature: aggSigBytes,
			}
			for b.Loop() {
				require.NoError(b, sig.Verify(unsignedMsg, constants.UnitTestID, canonicalValidators, 1, 1))
			}
		})
	}
}
