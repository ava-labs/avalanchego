// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"

	stdecdsa "crypto/ecdsa"
	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	// SignatureLen is the number of bytes in a secp2561k recoverable signature
	SignatureLen = 65

	// PrivateKeyLen is the number of bytes in a secp2561k recoverable private
	// key
	PrivateKeyLen = 32

	// PublicKeyLen is the number of bytes in a secp2561k recoverable public key
	PublicKeyLen = 33

	// from the decred library:
	// compactSigMagicOffset is a value used when creating the compact signature
	// recovery code inherited from Bitcoin and has no meaning, but has been
	// retained for compatibility.  For historical purposes, it was originally
	// picked to avoid a binary representation that would allow compact
	// signatures to be mistaken for other components.
	compactSigMagicOffset = 27

	PrivateKeyPrefix = "PrivateKey-"
	nullStr          = "null"
)

var (
	ErrInvalidSig              = errors.New("invalid signature")
	errCompressed              = errors.New("wasn't expecting a compressed key")
	errMissingQuotes           = errors.New("first and last characters should be quotes")
	errMissingKeyPrefix        = fmt.Errorf("private key missing %s prefix", PrivateKeyPrefix)
	errInvalidPrivateKeyLength = fmt.Errorf("private key has unexpected length, expected %d", PrivateKeyLen)
	errInvalidPublicKeyLength  = fmt.Errorf("public key has unexpected length, expected %d", PublicKeyLen)
	errInvalidSigLen           = errors.New("invalid signature length")
	errMutatedSig              = errors.New("signature was mutated from its original format")
)

func NewPrivateKey() (*PrivateKey, error) {
	k, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	return newPrivateKey(k), nil
}

func ToPublicKey(b []byte) (*PublicKey, error) {
	if len(b) != PublicKeyLen {
		return nil, errInvalidPublicKeyLength
	}

	key, err := secp256k1.ParsePubKey(b)
	if err != nil {
		return nil, err
	}
	return newPublicKey(key), nil
}

func ToPrivateKey(b []byte) (*PrivateKey, error) {
	if len(b) != PrivateKeyLen {
		return nil, errInvalidPrivateKeyLength
	}
	return newPrivateKey(secp256k1.PrivKeyFromBytes(b)), nil
}

func RecoverPublicKey(msg, sig []byte) (*PublicKey, error) {
	return RecoverPublicKeyFromHash(hashing.ComputeHash256(msg), sig)
}

func RecoverPublicKeyFromHash(hash, sig []byte) (*PublicKey, error) {
	if err := verifySECP256K1RSignatureFormat(sig); err != nil {
		return nil, err
	}

	sig, err := sigToRawSig(sig)
	if err != nil {
		return nil, err
	}

	rawPubkey, compressed, err := ecdsa.RecoverCompact(sig, hash)
	if err != nil {
		return nil, ErrInvalidSig
	}

	if compressed {
		return nil, errCompressed
	}

	return newPublicKey(rawPubkey), nil
}

type RecoverCache struct {
	cache cache.Cacher[ids.ID, *PublicKey]
}

func NewRecoverCache(size int) *RecoverCache {
	return &RecoverCache{
		cache: lru.NewCache[ids.ID, *PublicKey](size),
	}
}

func (r *RecoverCache) RecoverPublicKey(msg, sig []byte) (*PublicKey, error) {
	return r.RecoverPublicKeyFromHash(hashing.ComputeHash256(msg), sig)
}

func (r *RecoverCache) RecoverPublicKeyFromHash(hash, sig []byte) (*PublicKey, error) {
	// TODO: This type should always be initialized by calling NewRecoverCache.
	if r == nil || r.cache == nil {
		return RecoverPublicKeyFromHash(hash, sig)
	}

	cacheBytes := make([]byte, len(hash)+len(sig))
	copy(cacheBytes, hash)
	copy(cacheBytes[len(hash):], sig)
	id := hashing.ComputeHash256Array(cacheBytes)
	if cachedPublicKey, ok := r.cache.Get(id); ok {
		return cachedPublicKey, nil
	}

	pubKey, err := RecoverPublicKeyFromHash(hash, sig)
	if err != nil {
		return nil, err
	}

	r.cache.Put(id, pubKey)
	return pubKey, nil
}

// A PublicKey is an immutable secp256k1 public key. It is safe for concurrent
// use, which allows a single instance to be shared, e.g. via [RecoverCache].
type PublicKey struct {
	pk    *secp256k1.PublicKey
	addr  ids.ShortID
	bytes []byte
}

// newPublicKey computes the compressed serialization and address eagerly so
// that the returned key is never mutated after construction.
func newPublicKey(pk *secp256k1.PublicKey) *PublicKey {
	bytes := pk.SerializeCompressed()
	return &PublicKey{
		pk:    pk,
		addr:  ids.ShortID(hashing.ComputeHash160Array(hashing.ComputeHash256(bytes))),
		bytes: bytes,
	}
}

func (k *PublicKey) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

func (k *PublicKey) VerifyHash(hash, sig []byte) bool {
	pk, err := RecoverPublicKeyFromHash(hash, sig)
	if err != nil {
		return false
	}
	return k.Address() == pk.Address()
}

// ToECDSA returns the ecdsa representation of this public key
func (k *PublicKey) ToECDSA() *stdecdsa.PublicKey {
	return k.pk.ToECDSA()
}

func (k *PublicKey) Address() ids.ShortID {
	return k.addr
}

func (k *PublicKey) EthAddress() common.Address {
	return crypto.PubkeyToAddress(*(k.ToECDSA()))
}

// Bytes returns the compressed serialization of the key. The returned slice
// is shared and MUST NOT be modified.
func (k *PublicKey) Bytes() []byte {
	return k.bytes
}

// A PrivateKey is a secp256k1 private key. Its public key and serialization
// are computed at construction, so its accessors are safe for concurrent use.
type PrivateKey struct {
	sk    *secp256k1.PrivateKey
	pk    *PublicKey
	bytes []byte
}

// newPrivateKey computes the public key and serialization eagerly so that
// the returned key is never mutated after construction.
func newPrivateKey(sk *secp256k1.PrivateKey) *PrivateKey {
	return &PrivateKey{
		sk:    sk,
		pk:    newPublicKey(sk.PubKey()),
		bytes: sk.Serialize(),
	}
}

func (k *PrivateKey) PublicKey() *PublicKey {
	return k.pk
}

func (k *PrivateKey) Address() ids.ShortID {
	return k.PublicKey().Address()
}

func (k *PrivateKey) EthAddress() common.Address {
	return crypto.PubkeyToAddress(*(k.PublicKey().ToECDSA()))
}

func (k *PrivateKey) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

func (k *PrivateKey) SignHash(hash []byte) ([]byte, error) {
	sig := ecdsa.SignCompact(k.sk, hash, false) // returns [v || r || s]
	return rawSigToSig(sig)
}

// ToECDSA returns the ecdsa representation of this private key
func (k *PrivateKey) ToECDSA() *stdecdsa.PrivateKey {
	return k.sk.ToECDSA()
}

// Bytes returns the serialized private key. The returned slice is shared and
// MUST NOT be modified.
func (k *PrivateKey) Bytes() []byte {
	return k.bytes
}

func (k *PrivateKey) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of a SECP256K1 private key
	keyStr, _ := cb58.Encode(k.Bytes())
	return PrivateKeyPrefix + keyStr
}

func (k *PrivateKey) MarshalJSON() ([]byte, error) {
	return []byte(`"` + k.String() + `"`), nil
}

func (k *PrivateKey) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

func (k *PrivateKey) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	strNoQuotes := str[1:lastIndex]
	return k.unmarshalText(strNoQuotes)
}

func (k *PrivateKey) UnmarshalText(text []byte) error {
	return k.unmarshalText(string(text))
}

func (k *PrivateKey) unmarshalText(text string) error {
	if !strings.HasPrefix(text, PrivateKeyPrefix) {
		return errMissingKeyPrefix
	}

	strNoPrefix := text[len(PrivateKeyPrefix):]
	keyBytes, err := cb58.Decode(strNoPrefix)
	if err != nil {
		return err
	}
	if len(keyBytes) != PrivateKeyLen {
		return errInvalidPrivateKeyLength
	}

	*k = *newPrivateKey(secp256k1.PrivKeyFromBytes(keyBytes))
	return nil
}

// raw sig has format [v || r || s] whereas the sig has format [r || s || v]
func rawSigToSig(sig []byte) ([]byte, error) {
	if len(sig) != SignatureLen {
		return nil, errInvalidSigLen
	}
	recCode := sig[0]
	copy(sig, sig[1:])
	sig[SignatureLen-1] = recCode - compactSigMagicOffset
	return sig, nil
}

// sig has format [r || s || v] whereas the raw sig has format [v || r || s]
func sigToRawSig(sig []byte) ([]byte, error) {
	if len(sig) != SignatureLen {
		return nil, errInvalidSigLen
	}
	newSig := make([]byte, SignatureLen)
	newSig[0] = sig[SignatureLen-1] + compactSigMagicOffset //nolint:gosec // G602: length is validated above
	copy(newSig[1:], sig)
	return newSig, nil
}

// verifies the signature format in format [r || s || v]
func verifySECP256K1RSignatureFormat(sig []byte) error {
	if len(sig) != SignatureLen {
		return errInvalidSigLen
	}

	var s secp256k1.ModNScalar
	s.SetByteSlice(sig[32:64])
	if s.IsOverHalfOrder() {
		return errMutatedSig
	}
	return nil
}
