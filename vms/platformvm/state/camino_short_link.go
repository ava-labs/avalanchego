// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

type ShortLinkKey [12]byte

var ShortLinkKeyRegisterNode = ShortLinkKey{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

func (cs *caminoState) writeShortLinks() error {
	for nodeID, addr := range cs.modifiedShortLinks {
		delete(cs.modifiedShortLinks, nodeID)
		if addr == nil {
			if err := cs.shortLinksDB.Delete(nodeID[:]); err != nil {
				return err
			}
		} else {
			if err := cs.shortLinksDB.Put(nodeID[:], addr[:]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cs *caminoState) SetShortIDLink(id ids.ShortID, key ShortLinkKey, link *ids.ShortID) {
	linkKey := toShortLinkKey(id, key)
	cs.modifiedShortLinks[linkKey] = link
	cs.shortLinksCache.Evict(linkKey)
}

func (cs *caminoState) GetShortIDLink(id ids.ShortID, key ShortLinkKey) (ids.ShortID, error) {
	linkKey := toShortLinkKey(id, key)
	if addr, ok := cs.modifiedShortLinks[linkKey]; ok {
		if addr == nil {
			return ids.ShortEmpty, database.ErrNotFound
		}
		return *addr, nil
	}

	if addr, ok := cs.shortLinksCache.Get(linkKey); ok {
		if addr == nil {
			return ids.ShortEmpty, database.ErrNotFound
		}
		return *addr, nil
	}

	addrBytes, err := cs.shortLinksDB.Get(linkKey[:])
	if err == database.ErrNotFound {
		cs.shortLinksCache.Put(linkKey, nil)
		return ids.ShortEmpty, err
	} else if err != nil {
		return ids.ShortEmpty, err
	}

	linkedShortID, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ShortEmpty, err
	}

	cs.shortLinksCache.Put(linkKey, &linkedShortID)

	return linkedShortID, nil
}

func toShortLinkKey(id ids.ShortID, key ShortLinkKey) ids.ID {
	fullKey, _ := ids.ToID(append(key[:], id[:]...))
	return fullKey
}

func fromShortLinkKey(fullKey ids.ID) (ids.ShortID, ShortLinkKey) {
	id := ids.ShortID{}
	copy(id[:], fullKey[12:])
	key := ShortLinkKey{}
	copy(key[:], fullKey[:12])
	return id, key
}
