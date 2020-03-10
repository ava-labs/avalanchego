// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"bytes"
	"testing"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
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
			t.Fatalf("Failed signing with:\n%s", formatting.DumpBytes{Bytes: key.Bytes()})
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
