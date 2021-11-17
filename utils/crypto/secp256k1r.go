// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"bytes"
	"errors"
	"sort"

	stdecdsa "crypto/ecdsa"

	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v3"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	// SECP256K1RSigLen is the number of bytes in a secp2561k recoverable
	// signature
	SECP256K1RSigLen = 65

	// SECP256K1RSKLen is the number of bytes in a secp2561k recoverable private
	// key
	SECP256K1RSKLen = 32

	// SECP256K1RPKLen is the number of bytes in a secp2561k recoverable public
	// key
	SECP256K1RPKLen = 33

	// from the decred library:
	// compactSigMagicOffset is a value used when creating the compact signature
	// recovery code inherited from Bitcoin and has no meaning, but has been
	// retained for compatibility.  For historical purposes, it was originally
	// picked to avoid a binary representation that would allow compact
	// signatures to be mistaken for other components.
	compactSigMagicOffset = 27
)

var errCompressed = errors.New("wasn't expecting a compresses key")

type FactorySECP256K1R struct{ Cache cache.LRU }

// NewPrivateKey implements the Factory interface
func (*FactorySECP256K1R) NewPrivateKey() (PrivateKey, error) {
	k, err := secp256k1.GeneratePrivateKey()
	return &PrivateKeySECP256K1R{sk: k}, err
}

// ToPublicKey implements the Factory interface
func (*FactorySECP256K1R) ToPublicKey(b []byte) (PublicKey, error) {
	key, err := secp256k1.ParsePubKey(b)
	return &PublicKeySECP256K1R{
		pk:    key,
		bytes: b,
	}, err
}

// ToPrivateKey implements the Factory interface
func (*FactorySECP256K1R) ToPrivateKey(b []byte) (PrivateKey, error) {
	return &PrivateKeySECP256K1R{
		sk:    secp256k1.PrivKeyFromBytes(b),
		bytes: b,
	}, nil
}

// RecoverPublicKey returns the public key from a 65 byte signature
func (f *FactorySECP256K1R) RecoverPublicKey(msg, sig []byte) (PublicKey, error) {
	return f.RecoverHashPublicKey(hashing.ComputeHash256(msg), sig)
}

// RecoverHashPublicKey returns the public key from a 65 byte signature
func (f *FactorySECP256K1R) RecoverHashPublicKey(hash, sig []byte) (PublicKey, error) {
	cacheBytes := make([]byte, len(hash)+len(sig))
	copy(cacheBytes, hash)
	copy(cacheBytes[len(hash):], sig)
	id := hashing.ComputeHash256Array(cacheBytes)
	if cachedPublicKey, ok := f.Cache.Get(id); ok {
		return cachedPublicKey.(*PublicKeySECP256K1R), nil
	}

	if err := verifySECP256K1RSignatureFormat(sig); err != nil {
		return nil, err
	}

	sig, err := sigToRawSig(sig)
	if err != nil {
		return nil, err
	}

	rawPubkey, compressed, err := ecdsa.RecoverCompact(sig, hash)
	if err != nil {
		return nil, err
	}

	if compressed {
		return nil, errCompressed
	}

	pubkey := &PublicKeySECP256K1R{pk: rawPubkey}
	f.Cache.Put(id, pubkey)
	return pubkey, nil
}

type PublicKeySECP256K1R struct {
	pk    *secp256k1.PublicKey
	addr  ids.ShortID
	bytes []byte
}

// Verify implements the PublicKey interface
func (k *PublicKeySECP256K1R) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

// VerifyHash implements the PublicKey interface
func (k *PublicKeySECP256K1R) VerifyHash(hash, sig []byte) bool {
	factory := FactorySECP256K1R{}
	pk, err := factory.RecoverHashPublicKey(hash, sig)
	if err != nil {
		return false
	}
	return k.Address() == pk.Address()
}

// ToECDSA returns the ecdsa representation of this public key
func (k *PublicKeySECP256K1R) ToECDSA() *stdecdsa.PublicKey {
	return k.pk.ToECDSA()
}

// Address implements the PublicKey interface
func (k *PublicKeySECP256K1R) Address() ids.ShortID {
	if k.addr == ids.ShortEmpty {
		addr, err := ids.ToShortID(hashing.PubkeyBytesToAddress(k.Bytes()))
		if err != nil {
			panic(err)
		}
		k.addr = addr
	}
	return k.addr
}

// Bytes implements the PublicKey interface
func (k *PublicKeySECP256K1R) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = k.pk.SerializeCompressed()
	}
	return k.bytes
}

type PrivateKeySECP256K1R struct {
	sk    *secp256k1.PrivateKey
	pk    *PublicKeySECP256K1R
	bytes []byte
}

// PublicKey implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) PublicKey() PublicKey {
	if k.pk == nil {
		k.pk = &PublicKeySECP256K1R{pk: k.sk.PubKey()}
	}
	return k.pk
}

// Sign implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

// SignHash implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) SignHash(hash []byte) ([]byte, error) {
	sig := ecdsa.SignCompact(k.sk, hash, false) // returns [v || r || s]
	return rawSigToSig(sig)
}

// ToECDSA returns the ecdsa representation of this private key
func (k *PrivateKeySECP256K1R) ToECDSA() *stdecdsa.PrivateKey {
	return k.sk.ToECDSA()
}

// Bytes implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = k.sk.Serialize()
	}
	return k.bytes
}

// raw sig has format [v || r || s] whereas the sig has format [r || s || v]
func rawSigToSig(sig []byte) ([]byte, error) {
	if len(sig) != SECP256K1RSigLen {
		return nil, errInvalidSigLen
	}
	recCode := sig[0]
	copy(sig, sig[1:])
	sig[SECP256K1RSigLen-1] = recCode - compactSigMagicOffset
	return sig, nil
}

// sig has format [r || s || v] whereas the raw sig has format [v || r || s]
func sigToRawSig(sig []byte) ([]byte, error) {
	if len(sig) != SECP256K1RSigLen {
		return nil, errInvalidSigLen
	}
	newSig := make([]byte, SECP256K1RSigLen)
	newSig[0] = sig[SECP256K1RSigLen-1] + compactSigMagicOffset
	copy(newSig[1:], sig)
	return newSig, nil
}

// verifies the signature format in format [r || s || v]
func verifySECP256K1RSignatureFormat(sig []byte) error {
	if len(sig) != SECP256K1RSigLen {
		return errInvalidSigLen
	}

	var s secp256k1.ModNScalar
	s.SetByteSlice(sig[32:64])
	if s.IsOverHalfOrder() {
		return errMutatedSig
	}
	return nil
}

type innerSortSECP2561RSigs [][SECP256K1RSigLen]byte

func (lst innerSortSECP2561RSigs) Less(i, j int) bool { return bytes.Compare(lst[i][:], lst[j][:]) < 0 }
func (lst innerSortSECP2561RSigs) Len() int           { return len(lst) }
func (lst innerSortSECP2561RSigs) Swap(i, j int)      { lst[j], lst[i] = lst[i], lst[j] }

// SortSECP2561RSigs sorts a slice of SECP2561R signatures
func SortSECP2561RSigs(lst [][SECP256K1RSigLen]byte) { sort.Sort(innerSortSECP2561RSigs(lst)) }

// IsSortedAndUniqueSECP2561RSigs returns true if [sigs] is sorted
func IsSortedAndUniqueSECP2561RSigs(sigs [][SECP256K1RSigLen]byte) bool {
	return utils.IsSortedAndUnique(innerSortSECP2561RSigs(sigs))
}
