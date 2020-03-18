// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
)

var (
	errLockedFunds = errors.New("funds currently locked")
	errCantSpend   = errors.New("utxo couldn't be spent")
)

// Keychain is a collection of keys that can be used to spend utxos
type Keychain struct {
	factory   crypto.FactorySECP256K1R
	networkID uint32
	chainID   ids.ID

	// Key: The id of a private key (namely, [privKey].PublicKey().Address().Key())
	// Value: The index in Keys of that private key
	keyMap map[[20]byte]int

	// Each element is an address controlled by a key in [Keys]
	// This can be used to iterate over. It should not be modified externally.
	Addrs ids.ShortSet

	// List of keys this keychain manages
	// This can be used to iterate over. It should not be modified externally.
	Keys []*crypto.PrivateKeySECP256K1R
}

// NewKeychain creates a new keychain for a chain
func NewKeychain(networkID uint32, chainID ids.ID) *Keychain {
	return &Keychain{
		networkID: networkID,
		chainID:   chainID,
		keyMap:    make(map[[20]byte]int),
	}
}

// Add a new key to the key chain.
// If [key] is already in the keychain, does nothing.
func (kc *Keychain) Add(key *crypto.PrivateKeySECP256K1R) {
	addr := key.PublicKey().Address() // The address controlled by [key]
	addrHash := addr.Key()
	if _, ok := kc.keyMap[addrHash]; !ok {
		kc.keyMap[addrHash] = len(kc.Keys)
		kc.Keys = append(kc.Keys, key)
		kc.Addrs.Add(addr)
	}
}

// Get a key from the keychain. If the key is unknown, the second return value is false.
func (kc *Keychain) Get(id ids.ShortID) (*crypto.PrivateKeySECP256K1R, bool) {
	if i, ok := kc.keyMap[id.Key()]; ok {
		return kc.Keys[i], true
	}
	return &crypto.PrivateKeySECP256K1R{}, false
}

// Addresses returns a list of addresses this keychain manages
func (kc *Keychain) Addresses() ids.ShortSet { return kc.Addrs }

// New returns a newly generated private key.
// The key and the address it controls are added to
// [kc.Keys] and [kc.Addrs], respectively
func (kc *Keychain) New() (*crypto.PrivateKeySECP256K1R, error) {
	skGen, err := kc.factory.NewPrivateKey()
	if err != nil {
		return nil, err
	}

	sk := skGen.(*crypto.PrivateKeySECP256K1R)
	kc.Add(sk)
	return sk, nil
}

// Spend attempts to create an input
func (kc *Keychain) Spend(utxo *UTXO, time uint64) (Input, *InputSigner, error) {
	builder := Builder{
		NetworkID: kc.networkID,
		ChainID:   kc.chainID,
	}

	switch out := utxo.Out().(type) {
	case *OutputPayment:
		if time < out.Locktime() { // [UTXO] may not be spent yet
			return nil, nil, errLockedFunds
		}
		// Get [threshold] of the keys needed to spend [UTXO]
		if sigs, keys, able := kc.GetSigsAndKeys(out.Addresses(), int(out.Threshold())); able {
			sourceID, sourceIndex := utxo.Source()
			return builder.NewInputPayment(
					sourceID,
					sourceIndex,
					out.Amount(),
					sigs,
				),
				&InputSigner{Keys: keys},
				nil
		}
	case *OutputTakeOrLeave:
		if time < out.Locktime1() {
			return nil, nil, errLockedFunds
		}
		if sigs, keys, able := kc.GetSigsAndKeys(out.Addresses1(), int(out.Threshold1())); able {
			sourceID, sourceIndex := utxo.Source()
			return builder.NewInputPayment(
					sourceID,
					sourceIndex,
					out.Amount(),
					sigs,
				),
				&InputSigner{Keys: keys},
				nil
		}
		if time < out.Locktime2() {
			return nil, nil, errLockedFunds
		}
		if sigs, keys, able := kc.GetSigsAndKeys(out.Addresses2(), int(out.Threshold2())); able {
			sourceID, sourceIndex := utxo.Source()
			return builder.NewInputPayment(
					sourceID,
					sourceIndex,
					out.Amount(),
					sigs,
				),
				&InputSigner{Keys: keys},
				nil
		}
	}
	return nil, nil, errCantSpend
}

// GetSigsAndKeys returns:
// 1) A list of *Sig where [Sig].Index is the index of an address in [addresses]
//    such that a key in this keychain that controls the address
// 2) A list of private keys such that each key controls an address in [addresses]
// 3) true iff this keychain contains at least [threshold] keys that control an address
//    in [addresses]
func (kc *Keychain) GetSigsAndKeys(addresses []ids.ShortID, threshold int) ([]*Sig, []*crypto.PrivateKeySECP256K1R, bool) {
	sigs := []*Sig{}
	keys := []*crypto.PrivateKeySECP256K1R{}
	builder := Builder{
		NetworkID: kc.networkID,
		ChainID:   kc.chainID,
	}
	for i := uint32(0); i < uint32(len(addresses)) && len(keys) < threshold; i++ {
		if key, exists := kc.Get(addresses[i]); exists {
			sigs = append(sigs, builder.NewSig(i))
			keys = append(keys, key)
		}
	}
	return sigs, keys, len(keys) == threshold
}

// PrefixedString returns the key chain as a string representation with [prefix]
// added before every line.
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

func (kc *Keychain) String() string { return kc.PrefixedString("") }
