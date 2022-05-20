// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v3"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestRecover(t *testing.T) {
	f := FactorySECP256K1R{}
	key, _ := f.NewPrivateKey()

	msg := []byte{1, 2, 3}
	sig, _ := key.Sign(msg)

	pub := key.PublicKey()
	pubRec, _ := f.RecoverPublicKey(msg, sig)

	if !bytes.Equal(pub.Bytes(), pubRec.Bytes()) {
		t.Fatalf("Should have been equal")
	}
}

func TestCachedRecover(t *testing.T) {
	f := FactorySECP256K1R{Cache: cache.LRU{Size: 1}}
	key, _ := f.NewPrivateKey()

	msg := []byte{1, 2, 3}
	sig, _ := key.Sign(msg)

	pub1, _ := f.RecoverPublicKey(msg, sig)
	pub2, _ := f.RecoverPublicKey(msg, sig)

	if pub1 != pub2 {
		t.Fatalf("Should have returned the same public key")
	}
}

func TestExtensive(t *testing.T) {
	f := FactorySECP256K1R{}

	hash := hashing.ComputeHash256([]byte{1, 2, 3})
	for i := 0; i < 1000; i++ {
		if key, err := f.NewPrivateKey(); err != nil {
			t.Fatalf("Generated bad private key")
		} else if _, err := key.SignHash(hash); err != nil {
			t.Fatalf("Failed signing with:\n%s", formatting.DumpBytes(key.Bytes()))
		}
	}
}

func TestGenRecreate(t *testing.T) {
	f := FactorySECP256K1R{}

	for i := 0; i < 1000; i++ {
		sk, err := f.NewPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		skBytes := sk.Bytes()
		recoveredSk, err := f.ToPrivateKey(skBytes)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sk.PublicKey().Address().Bytes(), recoveredSk.PublicKey().Address().Bytes()) {
			t.Fatalf("Wrong public key")
		}
	}
}

func TestVerifyMutatedSignature(t *testing.T) {
	factory := FactorySECP256K1R{}

	sk, err := factory.NewPrivateKey()
	assert.NoError(t, err)

	msg := []byte{'h', 'e', 'l', 'l', 'o'}

	sig, err := sk.Sign(msg)
	assert.NoError(t, err)

	var s secp256k1.ModNScalar
	s.SetByteSlice(sig[32:64])
	s.Negate()
	newSBytes := s.Bytes()
	copy(sig[32:], newSBytes[:])

	_, err = factory.RecoverPublicKey(msg, sig)
	assert.Error(t, err)
}

/*
func TestPrivateKeyFromString(t *testing.T) {
	assert := assert.New(t)
	pk := PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	pkStr := pk.String()
	pk2, err := PrivateKeyFromString(pkStr)
	assert.NoError(err)
	assert.Equal(pk, pk2)
	expected := "PrivateKey-2qg4x8qM2s2qGNSXG"
	assert.Equal(expected, pkStr)
}

func TestPrivateKeyFromStringError(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		in string
	}{
		{""},
		{"foo"},
		{"foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := PrivateKeyFromString(tt.in)
			assert.Error(err)
		})
	}
}

func TestPrivateKeyMarshalJSON(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label string
		in    PrivateKey
		out   []byte
		err   error
	}{
		{"PrivateKey{}", PrivateKey{}, []byte("\"PrivateKey-45PJLL\""), nil},
		{
			"PrivateKey(\"ava labs\")",
			PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte("\"PrivateKey-2qg4x8qM2s2qGNSXG\""),
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			out, err := tt.in.MarshalJSON()
			assert.Equal(err, tt.err)
			assert.Equal(out, tt.out)
		})
	}
}

func TestPrivateKeyUnmarshalJSON(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label     string
		in        []byte
		out       PrivateKey
		shouldErr bool
	}{
		{"PrivateKey{}", []byte("null"), PrivateKey{}, false},
		{
			"PrivateKey(\"ava labs\")",
			[]byte("\"PrivateKey-2qg4x8qM2s2qGNSXG\""),
			PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			false,
		},
		{
			"missing start quote",
			[]byte("PrivateKey-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			PrivateKey{},
			true,
		},
		{
			"missing end quote",
			[]byte("\"PrivateKey-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"),
			PrivateKey{},
			true,
		},
		{
			"PrivateKey-",
			[]byte("\"PrivateKey-\""),
			PrivateKey{},
			true,
		},
		{
			"PrivateKey-1",
			[]byte("\"PrivateKey-1\""),
			PrivateKey{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			foo := PrivateKey{}
			fmt.Println(foo.String())
			err := foo.UnmarshalJSON(tt.in)
			if tt.shouldErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
			assert.Equal(foo, tt.out)
		})
	}
}

func TestPrivateKeyString(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label    string
		id       PrivateKey
		expected string
	}{
		{"PrivateKey{}", PrivateKey{}, "PrivateKey-45PJLL"},
		{"PrivateKey{24}", PrivateKey{24}, "PrivateKey-3nv2Q5L"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			result := tt.id.String()
			assert.Equal(result, tt.expected)
		})
	}
}

func TestSortPrivateKeys(t *testing.T) {
	assert := assert.New(t)
	pks := []PrivateKey{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	SortPrivateKeys(pks)
	expected := []PrivateKey{
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	assert.Equal(pks, expected)
}
*/
