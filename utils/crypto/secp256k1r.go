// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"sort"

	"github.com/ava-labs/go-ethereum/crypto"
	"github.com/ava-labs/go-ethereum/crypto/secp256k1"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/hashing"
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
)

// FactorySECP256K1R ...
type FactorySECP256K1R struct{ Cache cache.LRU }

// NewPrivateKey implements the Factory interface
func (*FactorySECP256K1R) NewPrivateKey() (PrivateKey, error) {
	k, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &PrivateKeySECP256K1R{sk: k}, nil
}

// ToPublicKey implements the Factory interface
func (*FactorySECP256K1R) ToPublicKey(b []byte) (PublicKey, error) {
	key, err := crypto.DecompressPubkey(b)
	return &PublicKeySECP256K1R{
		pk:    key,
		bytes: b,
	}, err
}

// ToPrivateKey implements the Factory interface
func (*FactorySECP256K1R) ToPrivateKey(b []byte) (PrivateKey, error) {
	key, err := crypto.ToECDSA(b)
	return &PrivateKeySECP256K1R{
		sk:    key,
		bytes: b,
	}, err
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
	id := ids.NewID(hashing.ComputeHash256Array(cacheBytes))
	if cachedPublicKey, ok := f.Cache.Get(id); ok {
		return cachedPublicKey.(*PublicKeySECP256K1), nil
	}

	if err := verifySECP256K1RSignatureFormat(sig); err != nil {
		return nil, err
	}

	rawPubkey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return nil, err
	}
	pubkey := &PublicKeySECP256K1{pk: rawPubkey}
	f.Cache.Put(id, pubkey)
	return pubkey, nil
}

// PublicKeySECP256K1R ...
type PublicKeySECP256K1R struct {
	pk    *ecdsa.PublicKey
	addr  ids.ShortID
	bytes []byte
}

// Verify implements the PublicKey interface
func (k *PublicKeySECP256K1R) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

// VerifyHash implements the PublicKey interface
func (k *PublicKeySECP256K1R) VerifyHash(hash, sig []byte) bool {
	if verifySECP256K1RSignatureFormat(sig) != nil {
		return false
	}
	return crypto.VerifySignature(k.Bytes(), hash, sig[:SECP256K1RSigLen-1])
}

// Address implements the PublicKey interface
func (k *PublicKeySECP256K1R) Address() ids.ShortID {
	if k.addr.IsZero() {
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
		k.bytes = crypto.CompressPubkey(k.pk)
	}
	return k.bytes
}

// PrivateKeySECP256K1R ...
type PrivateKeySECP256K1R struct {
	sk    *ecdsa.PrivateKey
	pk    *PublicKeySECP256K1R
	bytes []byte
}

// PublicKey implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) PublicKey() PublicKey {
	if k.pk == nil {
		k.pk = &PublicKeySECP256K1R{pk: (*ecdsa.PublicKey)(&k.sk.PublicKey)}
	}
	return k.pk
}

// Sign implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

// SignHash implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) SignHash(hash []byte) ([]byte, error) {
	return crypto.Sign(hash, k.sk)
}

// Bytes implements the PrivateKey interface
func (k *PrivateKeySECP256K1R) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = make([]byte, SECP256K1RSKLen)
		bytes := k.sk.D.Bytes()
		copy(k.bytes[SECP256K1RSKLen-len(bytes):], bytes)
	}
	return k.bytes
}

func verifySECP256K1RSignatureFormat(sig []byte) error {
	if len(sig) != SECP256K1RSigLen {
		return errInvalidSigLen
	}
	var r, s big.Int
	r.SetBytes(sig[:32])
	s.SetBytes(sig[32:64])
	if !crypto.ValidateSignatureValues(sig[64], &r, &s, true) {
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
