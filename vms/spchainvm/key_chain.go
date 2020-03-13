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

// Keychain is a collection of keys that can be used to spend utxos
type Keychain struct {
	networkID uint32
	chainID   ids.ID
	// This can be used to iterate over. However, it should not be modified externally.
	keyMap map[[20]byte]int
	Addrs  ids.ShortSet
	Keys   []*crypto.PrivateKeySECP256K1R
}

// NewKeychain creates a new keychain for a chain
func NewKeychain(networkID uint32, chainID ids.ID) *Keychain {
	return &Keychain{
		chainID: chainID,
		keyMap:  make(map[[20]byte]int),
	}
}

// New returns a newly generated private key
func (kc *Keychain) New() *crypto.PrivateKeySECP256K1R {
	factory := &crypto.FactorySECP256K1R{}

	skGen, _ := factory.NewPrivateKey()

	sk := skGen.(*crypto.PrivateKeySECP256K1R)
	kc.Add(sk)
	return sk
}

// Add a new key to the key chain
func (kc *Keychain) Add(key *crypto.PrivateKeySECP256K1R) {
	addr := key.PublicKey().Address()
	addrHash := addr.Key()
	if _, ok := kc.keyMap[addrHash]; !ok {
		kc.keyMap[addrHash] = len(kc.Keys)
		kc.Keys = append(kc.Keys, key)
		kc.Addrs.Add(addr)
	}
}

// Get a key from the keychain. If the key is unknown, the
func (kc *Keychain) Get(id ids.ShortID) (*crypto.PrivateKeySECP256K1R, bool) {
	if i, ok := kc.keyMap[id.Key()]; ok {
		return kc.Keys[i], true
	}
	return &crypto.PrivateKeySECP256K1R{}, false
}

// Addresses returns a list of addresses this keychain manages
func (kc *Keychain) Addresses() ids.ShortSet { return kc.Addrs }

// Spend attempts to create a new transaction
func (kc *Keychain) Spend(account Account, amount uint64, destination ids.ShortID) (*Tx, Account, error) {
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
func (kc *Keychain) PrefixedString(prefix string) string {
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

func (kc *Keychain) String() string {
	return kc.PrefixedString("")
}
