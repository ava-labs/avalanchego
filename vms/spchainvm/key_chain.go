// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
)

var (
	errUnknownAccount = errors.New("unknown account")
)

// KeyChain is a collection of keys that can be used to spend utxos
type KeyChain struct {
	networkID uint32
	chainID   ids.ID
	// This can be used to iterate over. However, it should not be modified externally.
	keyMap map[[20]byte]int
	Addrs  ids.ShortSet
	Keys   []*crypto.PrivateKeySECP256K1R
}

// NewKeyChain creates a new keychain for a chain
func NewKeyChain(networkID uint32, chainID ids.ID) *KeyChain {
	return &KeyChain{
		chainID: chainID,
		keyMap:  make(map[[20]byte]int),
	}
}

// New returns a newly generated private key
func (kc *KeyChain) New() *crypto.PrivateKeySECP256K1R {
	factory := &crypto.FactorySECP256K1R{}

	skGen, _ := factory.NewPrivateKey()

	sk := skGen.(*crypto.PrivateKeySECP256K1R)
	kc.Add(sk)
	return sk
}

// Add a new key to the key chain
func (kc *KeyChain) Add(key *crypto.PrivateKeySECP256K1R) {
	addr := key.PublicKey().Address()
	addrHash := addr.Key()
	if _, ok := kc.keyMap[addrHash]; !ok {
		kc.keyMap[addrHash] = len(kc.Keys)
		kc.Keys = append(kc.Keys, key)
		kc.Addrs.Add(addr)
	}
}

// Get a key from the keychain. If the key is unknown, the
func (kc *KeyChain) Get(id ids.ShortID) (*crypto.PrivateKeySECP256K1R, bool) {
	if i, ok := kc.keyMap[id.Key()]; ok {
		return kc.Keys[i], true
	}
	return &crypto.PrivateKeySECP256K1R{}, false
}

// Addresses returns a list of addresses this keychain manages
func (kc *KeyChain) Addresses() ids.ShortSet { return kc.Addrs }

// Spend attempts to create a new transaction
func (kc *KeyChain) Spend(account Account, amount uint64, destination ids.ShortID) (*Tx, Account, error) {
	key, exists := kc.Get(account.ID())
	if !exists {
		return nil, Account{}, errUnknownAccount
	}
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = kc.networkID
	ctx.ChainID = kc.chainID
	return account.CreateTx(amount, destination, ctx, key)
}

// PrefixedString returns a string representation of this keychain with each
// line prepended with [prefix]
func (kc *KeyChain) PrefixedString(prefix string) string {
	s := strings.Builder{}

	format := fmt.Sprintf("%%sKey[%s]: Key: %%s Address: %%s\n",
		formatting.IntFormat(len(kc.Keys)-1))
	for i, key := range kc.Keys {
		s.WriteString(fmt.Sprintf(format,
			prefix,
			i,
			formatting.CB58{Bytes: key.Bytes()},
			key.PublicKey().Address()))
	}

	return strings.TrimSuffix(s.String(), "\n")
}

func (kc *KeyChain) String() string {
	return kc.PrefixedString("")
}
