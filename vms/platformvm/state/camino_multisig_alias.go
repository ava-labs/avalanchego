// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
)

var errWrongOwnerType = errors.New("wrong owner type")

type msigAlias struct {
	Memo   types.JSONByteSlice `serialize:"true"`
	Owners verify.State        `serialize:"true"`
}

func (cs *caminoState) SetMultisigAlias(ma *multisig.Alias) {
	cs.modifiedMultisigOwners[ma.ID] = ma
}

func (cs *caminoState) GetMultisigAlias(id ids.ShortID) (*multisig.Alias, error) {
	if owner, exist := cs.modifiedMultisigOwners[id]; exist {
		if owner == nil {
			return nil, database.ErrNotFound
		}
		return owner, nil
	}

	maBytes, err := cs.multisigOwnersDB.Get(id[:])
	if err != nil {
		return nil, err
	}

	multisigAlias := &msigAlias{}
	if _, err = blocks.GenesisCodec.Unmarshal(maBytes, multisigAlias); err != nil {
		return nil, err
	}

	return &multisig.Alias{
		ID:     id,
		Memo:   multisigAlias.Memo,
		Owners: multisigAlias.Owners,
	}, nil
}

func (cs *caminoState) writeMultisigOwners() error {
	for key, alias := range cs.modifiedMultisigOwners {
		delete(cs.modifiedMultisigOwners, key)
		if alias == nil {
			if err := cs.multisigOwnersDB.Delete(key[:]); err != nil {
				return err
			}
		} else {
			multisigAlias := &msigAlias{
				Memo:   alias.Memo,
				Owners: alias.Owners,
			}
			aliasBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, multisigAlias)
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
