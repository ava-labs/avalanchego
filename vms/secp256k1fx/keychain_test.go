// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var (
	keys = []string{
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
	}
	addrs = []string{
		"B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW",
		"P5wdRuZeaDt28eHMP5S3w9ZdoBfo7wuzF",
		"Q4MzFZZDPHRPAHFeDs3NiyyaZDvxHKivf",
	}
)

func TestNewKeychain(t *testing.T) {
	kc := NewKeychain()
	if kc == nil {
		t.Fatalf("NewKeychain returned a nil keychain")
	}
}

func TestKeychainGetUnknownAddr(t *testing.T) {
	kc := NewKeychain()

	addr, _ := ids.ShortFromString(addrs[0])
	if _, exists := kc.Get(addr); exists {
		t.Fatalf("Shouldn't have returned a key from an empty keychain")
	}
}

func TestKeychainAdd(t *testing.T) {
	kc := NewKeychain()

	cb58 := formatting.CB58{}
	if err := cb58.FromString(keys[0]); err != nil {
		t.Fatal(err)
	}
	skBytes := cb58.Bytes

	skIntff, err := kc.factory.ToPrivateKey(skBytes)
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := skIntff.(*crypto.PrivateKeySECP256K1R)
	if !ok {
		t.Fatalf("Factory should have returned secp256k1r private key")
	}

	kc.Add(sk)

	addr, _ := ids.ShortFromString(addrs[0])
	if rsk, exists := kc.Get(addr); !exists {
		t.Fatalf("Should have returned the key from the keychain")
	} else if !bytes.Equal(rsk.Bytes(), sk.Bytes()) {
		t.Fatalf("Returned wrong key from the keychain")
	}

	if addrs := kc.Addresses(); addrs.Len() != 1 {
		t.Fatalf("Should have returned one address from the keychain")
	} else if !addrs.Contains(addr) {
		t.Fatalf("Keychain contains the wrong address")
	}
}

func TestKeychainNew(t *testing.T) {
	kc := NewKeychain()

	if addrs := kc.Addresses(); addrs.Len() != 0 {
		t.Fatalf("Shouldn't have returned any addresses from the empty keychain")
	}

	sk, err := kc.New()
	if err != nil {
		t.Fatal(err)
	}

	addr := sk.PublicKey().Address()

	if addrs := kc.Addresses(); addrs.Len() != 1 {
		t.Fatalf("Should have returned one address from the keychain")
	} else if !addrs.Contains(addr) {
		t.Fatalf("Keychain contains the wrong address")
	}
}

func TestKeychainMatch(t *testing.T) {
	kc := NewKeychain()

	cb58 := formatting.CB58{}
	sks := []*crypto.PrivateKeySECP256K1R{}
	for _, keyStr := range keys {
		if err := cb58.FromString(keyStr); err != nil {
			t.Fatal(err)
		}
		skBytes := cb58.Bytes

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		if err != nil {
			t.Fatal(err)
		}
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		if !ok {
			t.Fatalf("Factory should have returned secp256k1r private key")
		}
		sks = append(sks, sk)
	}

	kc.Add(sks[0])

	owners := OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			sks[1].PublicKey().Address(),
			sks[2].PublicKey().Address(),
		},
	}
	if err := owners.Verify(); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := kc.Match(&owners, 0); ok {
		t.Fatalf("Shouldn't have been able to match with the owners")
	}

	kc.Add(sks[1])

	if indices, keys, ok := kc.Match(&owners, 1); !ok {
		t.Fatalf("Should have been able to match with the owners")
	} else if numIndices := len(indices); numIndices != 1 {
		t.Fatalf("Should have returned one index")
	} else if numKeys := len(keys); numKeys != 1 {
		t.Fatalf("Should have returned one key")
	} else if index := indices[0]; index != 0 {
		t.Fatalf("Should have returned index 0 for the key")
	} else if key := keys[0]; !key.PublicKey().Address().Equals(sks[1].PublicKey().Address()) {
		t.Fatalf("Returned wrong key")
	}

	kc.Add(sks[2])

	if indices, keys, ok := kc.Match(&owners, 1); !ok {
		t.Fatalf("Should have been able to match with the owners")
	} else if numIndices := len(indices); numIndices != 1 {
		t.Fatalf("Should have returned one index")
	} else if numKeys := len(keys); numKeys != 1 {
		t.Fatalf("Should have returned one key")
	} else if index := indices[0]; index != 0 {
		t.Fatalf("Should have returned index 0 for the key")
	} else if key := keys[0]; !key.PublicKey().Address().Equals(sks[1].PublicKey().Address()) {
		t.Fatalf("Returned wrong key")
	}
}

func TestKeychainSpendMint(t *testing.T) {
	kc := NewKeychain()

	cb58 := formatting.CB58{}
	sks := []*crypto.PrivateKeySECP256K1R{}
	for _, keyStr := range keys {
		if err := cb58.FromString(keyStr); err != nil {
			t.Fatal(err)
		}
		skBytes := cb58.Bytes

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		if err != nil {
			t.Fatal(err)
		}
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		if !ok {
			t.Fatalf("Factory should have returned secp256k1r private key")
		}
		sks = append(sks, sk)
	}

	mint := MintOutput{OutputOwners: OutputOwners{
		Threshold: 2,
		Addrs: []ids.ShortID{
			sks[1].PublicKey().Address(),
			sks[2].PublicKey().Address(),
		},
	}}
	if err := mint.Verify(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := kc.Spend(&mint, 0); err == nil {
		t.Fatalf("Shouldn't have been able to spend with no keys")
	}

	kc.Add(sks[0])
	kc.Add(sks[1])
	kc.Add(sks[2])

	if input, keys, err := kc.Spend(&mint, 0); err != nil {
		t.Fatal(err)
	} else if input, ok := input.(*Input); !ok {
		t.Fatalf("Wrong input type returned")
	} else if err := input.Verify(); err != nil {
		t.Fatal(err)
	} else if numSigs := len(input.SigIndices); numSigs != 2 {
		t.Fatalf("Should have returned two signers")
	} else if sig := input.SigIndices[0]; sig != 0 {
		t.Fatalf("Should have returned index of secret key 1")
	} else if sig := input.SigIndices[1]; sig != 1 {
		t.Fatalf("Should have returned index of secret key 2")
	} else if numKeys := len(keys); numKeys != 2 {
		t.Fatalf("Should have returned two keys")
	} else if key := keys[0]; !key.PublicKey().Address().Equals(sks[1].PublicKey().Address()) {
		t.Fatalf("Returned wrong key")
	} else if key := keys[1]; !key.PublicKey().Address().Equals(sks[2].PublicKey().Address()) {
		t.Fatalf("Returned wrong key")
	}
}

func TestKeychainSpendTransfer(t *testing.T) {
	kc := NewKeychain()

	cb58 := formatting.CB58{}
	sks := []*crypto.PrivateKeySECP256K1R{}
	for _, keyStr := range keys {
		if err := cb58.FromString(keyStr); err != nil {
			t.Fatal(err)
		}
		skBytes := cb58.Bytes

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		if err != nil {
			t.Fatal(err)
		}
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		if !ok {
			t.Fatalf("Factory should have returned secp256k1r private key")
		}
		sks = append(sks, sk)
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 2,
			Addrs: []ids.ShortID{
				sks[1].PublicKey().Address(),
				sks[2].PublicKey().Address(),
			},
		},
	}
	if err := transfer.Verify(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := kc.Spend(&transfer, 54321); err == nil {
		t.Fatalf("Shouldn't have been able to spend with no keys")
	}

	kc.Add(sks[0])
	kc.Add(sks[1])
	kc.Add(sks[2])

	if _, _, err := kc.Spend(&transfer, 4321); err == nil {
		t.Fatalf("Shouldn't have been able timelocked funds")
	}

	if input, keys, err := kc.Spend(&transfer, 54321); err != nil {
		t.Fatal(err)
	} else if input, ok := input.(*TransferInput); !ok {
		t.Fatalf("Wrong input type returned")
	} else if err := input.Verify(); err != nil {
		t.Fatal(err)
	} else if amt := input.Amount(); amt != 12345 {
		t.Fatalf("Wrong amount returned from input")
	} else if numSigs := len(input.SigIndices); numSigs != 2 {
		t.Fatalf("Should have returned two signers")
	} else if sig := input.SigIndices[0]; sig != 0 {
		t.Fatalf("Should have returned index of secret key 1")
	} else if sig := input.SigIndices[1]; sig != 1 {
		t.Fatalf("Should have returned index of secret key 2")
	} else if numKeys := len(keys); numKeys != 2 {
		t.Fatalf("Should have returned two keys")
	} else if key := keys[0]; !key.PublicKey().Address().Equals(sks[1].PublicKey().Address()) {
		t.Fatalf("Returned wrong key")
	} else if key := keys[1]; !key.PublicKey().Address().Equals(sks[2].PublicKey().Address()) {
		t.Fatalf("Returned wrong key")
	}
}

func TestKeychainString(t *testing.T) {
	kc := NewKeychain()

	cb58 := formatting.CB58{}
	if err := cb58.FromString(keys[0]); err != nil {
		t.Fatal(err)
	}
	skBytes := cb58.Bytes

	skIntf, err := kc.factory.ToPrivateKey(skBytes)
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
	if !ok {
		t.Fatalf("Factory should have returned secp256k1r private key")
	}

	kc.Add(sk)

	expected := "Key[0]: Key: 2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY Address: B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW"

	if result := kc.String(); result != expected {
		t.Fatalf("Keychain.String returned:\n%s\nexpected:\n%s", result, expected)
	}
}

func TestKeychainPrefixedString(t *testing.T) {
	kc := NewKeychain()

	cb58 := formatting.CB58{}
	if err := cb58.FromString(keys[0]); err != nil {
		t.Fatal(err)
	}
	skBytes := cb58.Bytes

	skIntf, err := kc.factory.ToPrivateKey(skBytes)
	if err != nil {
		t.Fatal(err)
	}
	sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
	if !ok {
		t.Fatalf("Factory should have returned secp256k1r private key")
	}

	kc.Add(sk)

	expected := "xDKey[0]: Key: 2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY Address: B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW"

	if result := kc.PrefixedString("xD"); result != expected {
		t.Fatalf(`Keychain.PrefixedString("xD") returned:\n%s\nexpected:\n%s`, result, expected)
	}
}
