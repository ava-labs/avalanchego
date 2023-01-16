// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var errWrongOwnerType = errors.New("wrong owner type")

func FromGenesisMultisigAlias(msig genesis.MultisigAlias) *multisig.Alias {
	// Important! OutputOwners expects sorted list of addresses
	owners := msig.Addresses
	utils.Sort(owners)

	return &multisig.Alias{
		ID:   msig.Alias,
		Memo: types.JSONByteSlice(msig.Memo),
		Owners: &secp256k1fx.OutputOwners{
			Threshold: msig.Threshold,
			Addrs:     owners,
		},
	}
}

func (cs *caminoState) SetMultisigAlias(ma *multisig.Alias) {
	cs.modifiedMultisigOwners[ma.ID] = ma
}

func (cs *caminoState) GetMultisigAlias(alias ids.ShortID) (*multisig.Alias, error) {
	if owner, exist := cs.modifiedMultisigOwners[alias]; exist {
		return owner, nil
	}

	maBytes, err := cs.multisigOwnersDB.Get(alias[:])
	if err != nil {
		return nil, err
	}

	multisigAlias := &multisig.Alias{}
	_, err = blocks.GenesisCodec.Unmarshal(maBytes, multisigAlias)
	if err != nil {
		return nil, err
	}

	multisigAlias.ID = alias

	return multisigAlias, nil
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

func GetOwner(state Chain, addr ids.ShortID) (*secp256k1fx.OutputOwners, error) {
	msigOwner, err := state.GetMultisigAlias(addr)
	if err != nil && err != database.ErrNotFound {
		return nil, err
	}

	if msigOwner != nil {
		owners, ok := msigOwner.Owners.(*secp256k1fx.OutputOwners)
		if !ok {
			return nil, errWrongOwnerType
		}
		return owners, nil
	}

	return &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}, nil
}
