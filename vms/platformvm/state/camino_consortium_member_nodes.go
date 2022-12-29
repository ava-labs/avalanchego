// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

func (cs *caminoState) writeNodeConsortiumMembers() error {
	for nodeID, addr := range cs.modifiedConsortiumMemberNodes {
		delete(cs.modifiedConsortiumMemberNodes, nodeID)
		if addr == nil {
			if err := cs.consortiumMemberNodesDB.Delete(nodeID[:]); err != nil {
				return err
			}
		} else {
			if err := cs.consortiumMemberNodesDB.Put(nodeID[:], addr[:]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cs *caminoState) SetNodeConsortiumMember(nodeID ids.NodeID, addr *ids.ShortID) {
	cs.modifiedConsortiumMemberNodes[nodeID] = addr
}

func (cs *caminoState) GetNodeConsortiumMember(nodeID ids.NodeID) (ids.ShortID, error) {
	if addr, ok := cs.modifiedConsortiumMemberNodes[nodeID]; ok {
		if addr == nil {
			return ids.ShortEmpty, database.ErrNotFound
		}
		return *addr, nil
	}

	if addr, ok := cs.consortiumMemberNodesCache.Get(nodeID); ok {
		return addr.(ids.ShortID), nil
	}

	addrBytes, err := cs.consortiumMemberNodesDB.Get(nodeID[:])
	if err != nil {
		return ids.ShortEmpty, err
	}

	return ids.ToShortID(addrBytes)
}
