// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/crypto"
)

var addresses = ids.Empty

type userState struct{ vm *VM }

func (s *userState) SetAddresses(db database.Database, addrs []ids.ShortID) error {
	bytes, err := s.vm.codec.Marshal(addrs)
	if err != nil {
		return err
	}
	return db.Put(addresses.Bytes(), bytes)
}

func (s *userState) Addresses(db database.Database) ([]ids.ShortID, error) {
	bytes, err := db.Get(addresses.Bytes())
	if err != nil {
		return nil, err
	}
	addresses := []ids.ShortID{}
	if err := s.vm.codec.Unmarshal(bytes, &addresses); err != nil {
		return nil, err
	}
	return addresses, nil
}

func (s *userState) SetKey(db database.Database, sk *crypto.PrivateKeySECP256K1R) error {
	return db.Put(sk.PublicKey().Address().Bytes(), sk.Bytes())
}

func (s *userState) Key(db database.Database, address ids.ShortID) (*crypto.PrivateKeySECP256K1R, error) {
	factory := crypto.FactorySECP256K1R{}

	bytes, err := db.Get(address.Bytes())
	if err != nil {
		return nil, err
	}
	sk, err := factory.ToPrivateKey(bytes)
	if err != nil {
		return nil, err
	}
	return sk.(*crypto.PrivateKeySECP256K1R), nil
}
