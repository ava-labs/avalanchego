// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ Signature = (*BitSetSignature)(nil)

	ErrInvalidBitSet      = errors.New("bitset is invalid")
	ErrInsufficientWeight = errors.New("signature weight is insufficient")
	ErrInvalidSignature   = errors.New("signature is invalid")
	ErrParseSignature     = errors.New("failed to parse signature")
)

type Signature interface {
	fmt.Stringer

	// NumSigners is the number of [bls.PublicKeys] that participated in the
	// [Signature]. This is exposed because users of these signatures typically
	// impose a verification fee that is a function of the number of
	// signers.
	NumSigners() (int, error)

	// Verify that this signature was signed by at least [quorumNum]/[quorumDen]
	// of the validators of [msg.SourceChainID] at [pChainHeight].
	//
	// Invariant: [msg] is correctly initialized.
	Verify(
		msg *UnsignedMessage,
		networkID uint32,
		validators validators.WarpSet,
		quorumNum uint64,
		quorumDen uint64,
	) error
}

type BitSetSignature struct {
	// Signers is a big-endian byte slice encoding which validators signed this
	// message.
	Signers   []byte                 `serialize:"true"`
	Signature [bls.SignatureLen]byte `serialize:"true"`
}

func (s *BitSetSignature) NumSigners() (int, error) {
	// Parse signer bit vector
	//
	// We assert that the length of [signerIndices.Bytes()] is equal
	// to [len(s.Signers)] to ensure that [s.Signers] does not have
	// any unnecessary zero-padding to represent the [set.Bits].
	signerIndices := set.BitsFromBytes(s.Signers)
	if len(signerIndices.Bytes()) != len(s.Signers) {
		return 0, ErrInvalidBitSet
	}
	return signerIndices.Len(), nil
}

func (s *BitSetSignature) Verify(
	msg *UnsignedMessage,
	networkID uint32,
	validators validators.WarpSet,
	quorumNum uint64,
	quorumDen uint64,
) error {
	if msg.NetworkID != networkID {
		return ErrWrongNetworkID
	}

	// Parse signer bit vector
	//
	// We assert that the length of [signerIndices.Bytes()] is equal
	// to [len(s.Signers)] to ensure that [s.Signers] does not have
	// any unnecessary zero-padding to represent the [set.Bits].
	signerIndices := set.BitsFromBytes(s.Signers)
	if len(signerIndices.Bytes()) != len(s.Signers) {
		return ErrInvalidBitSet
	}

	// Get the validators that (allegedly) signed the message.
	signers, err := FilterValidators(signerIndices, validators.Validators)
	if err != nil {
		return err
	}

	// Because [signers] is a subset of [validators.Validators], this can never error.
	sigWeight, _ := SumWeight(signers)

	// Make sure the signature's weight is sufficient.
	err = VerifyWeight(
		sigWeight,
		validators.TotalWeight,
		quorumNum,
		quorumDen,
	)
	if err != nil {
		return err
	}

	// Parse the aggregate signature
	aggSig, err := bls.SignatureFromBytes(s.Signature[:])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseSignature, err)
	}

	// Create the aggregate public key
	aggPubKey, err := AggregatePublicKeys(signers)
	if err != nil {
		return err
	}

	// Verify the signature
	unsignedBytes := msg.Bytes()
	if !bls.Verify(aggPubKey, aggSig, unsignedBytes) {
		return ErrInvalidSignature
	}
	return nil
}

func (s *BitSetSignature) String() string {
	return fmt.Sprintf("BitSetSignature(Signers = %x, Signature = %x)", s.Signers, s.Signature)
}

// VerifyWeight returns [nil] if [sigWeight] is at least [quorumNum]/[quorumDen]
// of [totalWeight].
// If [sigWeight >= totalWeight * quorumNum / quorumDen] then return [nil]
func VerifyWeight(
	sigWeight uint64,
	totalWeight uint64,
	quorumNum uint64,
	quorumDen uint64,
) error {
	// Verifies that quorumNum * totalWeight <= quorumDen * sigWeight
	scaledTotalWeight := new(big.Int).SetUint64(totalWeight)
	scaledTotalWeight.Mul(scaledTotalWeight, new(big.Int).SetUint64(quorumNum))
	scaledSigWeight := new(big.Int).SetUint64(sigWeight)
	scaledSigWeight.Mul(scaledSigWeight, new(big.Int).SetUint64(quorumDen))
	if scaledTotalWeight.Cmp(scaledSigWeight) == 1 {
		return fmt.Errorf(
			"%w: %d*%d > %d*%d",
			ErrInsufficientWeight,
			quorumNum,
			totalWeight,
			quorumDen,
			sigWeight,
		)
	}
	return nil
}
