// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
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
	Nonce  uint64              `serialize:"true"`
}

func (cs *caminoState) SetMultisigAlias(ma *multisig.AliasWithNonce) {
	cs.modifiedMultisigAliases[ma.ID] = ma
	cs.multisigAliasesCache.Evict(ma.ID)
}

func (cs *caminoState) GetMultisigAlias(id ids.ShortID) (*multisig.AliasWithNonce, error) {
	if owner, exist := cs.modifiedMultisigAliases[id]; exist {
		if owner == nil {
			return nil, database.ErrNotFound
		}
		return owner, nil
	}

	if alias, exist := cs.multisigAliasesCache.Get(id); exist {
		if alias == nil {
			return nil, database.ErrNotFound
		}
		return alias, nil
	}

	maBytes, err := cs.multisigAliasesDB.Get(id[:])
	if err == database.ErrNotFound {
		cs.multisigAliasesCache.Put(id, nil)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	dbMultisigAlias := &msigAlias{}
	if _, err = blocks.GenesisCodec.Unmarshal(maBytes, dbMultisigAlias); err != nil {
		return nil, err
	}

	msigAlias := &multisig.AliasWithNonce{
		Alias: multisig.Alias{
			ID:     id,
			Memo:   dbMultisigAlias.Memo,
			Owners: dbMultisigAlias.Owners,
		},
		Nonce: dbMultisigAlias.Nonce,
	}

	cs.multisigAliasesCache.Put(id, msigAlias)
	return msigAlias, nil
}

func (cs *caminoState) writeMultisigAliases() error {
	for key, alias := range cs.modifiedMultisigAliases {
		delete(cs.modifiedMultisigAliases, key)
		if alias == nil {
			if err := cs.multisigAliasesDB.Delete(key[:]); err != nil {
				return err
			}
		} else {
			multisigAlias := &msigAlias{
				Memo:   alias.Memo,
				Owners: alias.Owners,
				Nonce:  alias.Nonce,
			}
			aliasBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, multisigAlias)
			if err != nil {
				return fmt.Errorf("failed to serialize multisig alias: %w", err)
			}
			if err := cs.multisigAliasesDB.Put(key[:], aliasBytes); err != nil {
				return err
			}
		}
	}
	return nil
}
