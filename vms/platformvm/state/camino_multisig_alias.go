// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type MultisigOwner struct {
	Alias  ids.ShortID
	Owners secp256k1fx.OutputOwners `serialize:"true" json:"owners"`
}

func FromGenesisMultisigAlias(gma genesis.MultisigAlias) *MultisigOwner {
	// Important! OutputOwners expects sorted list of addresses
	owners := gma.Addresses
	utils.Sort(owners)

	return &MultisigOwner{
		Alias: gma.Alias,
		Owners: secp256k1fx.OutputOwners{
			Threshold: gma.Threshold,
			Addrs:     owners,
		},
	}
}

func (cs *caminoState) SetMultisigOwner(ma *MultisigOwner) {
	cs.modifiedMultisigOwners[ma.Alias] = ma
}

func (cs *caminoState) GetMultisigOwner(alias ids.ShortID) (*MultisigOwner, error) {
	if owner, exist := cs.modifiedMultisigOwners[alias]; exist {
		return owner, nil
	}

	multisigAlias := &MultisigOwner{}
	maBytes, err := cs.multisigOwnersDB.Get(alias.Bytes())
	if err != nil {
		return multisigAlias, err
	}

	_, err = blocks.GenesisCodec.Unmarshal(maBytes, multisigAlias)
	multisigAlias.Alias = alias

	return multisigAlias, err
}

func (cs *caminoState) writeMultisigOwners() error {
	for key, alias := range cs.modifiedMultisigOwners {
		delete(cs.modifiedMultisigOwners, key)
		if alias == nil {
			if err := cs.multisigOwnersDB.Delete(key[:]); err != nil {
				return err
			}
		} else {
			aliasBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, alias)
			if err != nil {
				return fmt.Errorf("failed to serialize multisig alias: %w", err)
			}
			if err := cs.multisigOwnersDB.Put(key[:], aliasBytes); err != nil {
				return err
			}
		}
	}
	return nil
}
