// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/types"
)

type msigAlias struct {
	Memo   types.JSONByteSlice `serialize:"true"`
	Owners verify.State        `serialize:"true"`
}

func (cs *caminoState) SetMultisigAlias(ma *multisig.Alias) {
	cs.modifiedMultisigOwners[ma.ID] = ma
	cs.multisigOwnersCache.Evict(ma.ID)
}

func (cs *caminoState) GetMultisigAlias(id ids.ShortID) (*multisig.Alias, error) {
	if owner, exist := cs.modifiedMultisigOwners[id]; exist {
		if owner == nil {
			return nil, database.ErrNotFound
		}
		return owner, nil
	}

	if alias, exist := cs.multisigOwnersCache.Get(id); exist {
		if alias == nil {
			return nil, database.ErrNotFound
		}
		return alias.(*multisig.Alias), nil
	}

	maBytes, err := cs.multisigOwnersDB.Get(id[:])
	if err == database.ErrNotFound {
		cs.multisigOwnersCache.Put(id, nil)
		return nil, err
	} else if err != nil {
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
