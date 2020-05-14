// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"fmt"
	"sync"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils"
)

// Connections provides an interface for what a group of connections will
// support.
type Connections interface {
	Add(salticidae.PeerID, ids.ShortID, utils.IPDesc)

	GetPeerID(ids.ShortID) (salticidae.PeerID, bool)
	GetID(salticidae.PeerID) (ids.ShortID, bool)

	ContainsPeerID(salticidae.PeerID) bool
	ContainsID(ids.ShortID) bool
	ContainsIP(utils.IPDesc) bool

	Remove(salticidae.PeerID, ids.ShortID)
	RemovePeerID(salticidae.PeerID)
	RemoveID(ids.ShortID)

	PeerIDs() []salticidae.PeerID
	IDs() ids.ShortSet
	IPs() []utils.IPDesc
	Conns() ([]salticidae.PeerID, []ids.ShortID, []utils.IPDesc)

	Len() int
}

type connections struct {
	mux sync.Mutex
	// peerID -> id
	peerIDToID map[[32]byte]ids.ShortID
	// id -> peerID
	idToPeerID map[[20]byte]salticidae.PeerID
	// id -> ip
	idToIP map[[20]byte]utils.IPDesc
}

// NewConnections returns a new and empty connections object
func NewConnections() Connections {
	return &connections{
		peerIDToID: make(map[[32]byte]ids.ShortID),
		idToPeerID: make(map[[20]byte]salticidae.PeerID),
		idToIP:     make(map[[20]byte]utils.IPDesc),
	}
}

// Add Assumes that peer is garbage collected normally
func (c *connections) Add(peer salticidae.PeerID, id ids.ShortID, ip utils.IPDesc) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.add(peer, id, ip)
}

// GetPeerID returns the peer mapped to the id that is provided if one exists.
func (c *connections) GetPeerID(id ids.ShortID) (salticidae.PeerID, bool) {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.getPeerID(id)
}

// GetID returns the id mapped to the peer that is provided if one exists.
func (c *connections) GetID(peer salticidae.PeerID) (ids.ShortID, bool) {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.getID(peer)
}

// ContainsPeerID returns true if the peer is contained in the connection pool
func (c *connections) ContainsPeerID(peer salticidae.PeerID) bool {
	_, exists := c.GetID(peer)
	return exists
}

// ContainsID returns true if the id is contained in the connection pool
func (c *connections) ContainsID(id ids.ShortID) bool {
	_, exists := c.GetPeerID(id)
	return exists
}

// ContainsIP returns true if the ip is contained in the connection pool
func (c *connections) ContainsIP(ip utils.IPDesc) bool {
	for _, otherIP := range c.IPs() {
		if ip.Equal(otherIP) {
			return true
		}
	}
	return false
}

// Remove ensures that no connection will have any mapping containing [peer] or
// [id].
func (c *connections) Remove(peer salticidae.PeerID, id ids.ShortID) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.remove(peer, id)
}

// RemovePeerID ensures that no connection will have a mapping containing [peer]
func (c *connections) RemovePeerID(peer salticidae.PeerID) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.removePeerID(peer)
}

// RemoveID ensures that no connection will have a mapping containing [id]
func (c *connections) RemoveID(id ids.ShortID) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.removeID(id)
}

// PeerIDs returns the full list of peers contained in this connection pool.
func (c *connections) PeerIDs() []salticidae.PeerID {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.peerIDs()
}

// IDs return the set of IDs that are mapping in this connection pool.
func (c *connections) IDs() ids.ShortSet {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.ids()
}

// IPs return the set of IPs that are mapped in this connection pool.
func (c *connections) IPs() []utils.IPDesc {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.ips()
}

// Conns return the set of connections in this connection pool.
func (c *connections) Conns() ([]salticidae.PeerID, []ids.ShortID, []utils.IPDesc) {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.conns()
}

// Len returns the number of elements in the map
func (c *connections) Len() int {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.len()
}

func (c *connections) add(peer salticidae.PeerID, id ids.ShortID, ip utils.IPDesc) {
	c.remove(peer, id)

	key := id.Key()
	c.peerIDToID[toID(peer)] = id
	c.idToPeerID[key] = peer
	c.idToIP[key] = ip
}

func (c *connections) getPeerID(id ids.ShortID) (salticidae.PeerID, bool) {
	peer, exists := c.idToPeerID[id.Key()]
	return peer, exists
}

func (c *connections) getID(peer salticidae.PeerID) (ids.ShortID, bool) {
	id, exists := c.peerIDToID[toID(peer)]
	return id, exists
}

func (c *connections) remove(peer salticidae.PeerID, id ids.ShortID) {
	c.removePeerID(peer)
	c.removeID(id)
}

func (c *connections) removePeerID(peer salticidae.PeerID) {
	peerID := toID(peer)
	if id, exists := c.peerIDToID[peerID]; exists {
		idKey := id.Key()

		delete(c.peerIDToID, peerID)
		delete(c.idToPeerID, idKey)
		delete(c.idToIP, idKey)
	}
}

func (c *connections) removeID(id ids.ShortID) {
	idKey := id.Key()
	if peer, exists := c.idToPeerID[idKey]; exists {
		delete(c.peerIDToID, toID(peer))
		delete(c.idToPeerID, idKey)
		delete(c.idToIP, idKey)
	}
}

func (c *connections) peerIDs() []salticidae.PeerID {
	peers := make([]salticidae.PeerID, 0, len(c.idToPeerID))
	for _, peer := range c.idToPeerID {
		peers = append(peers, peer)
	}
	return peers
}

func (c *connections) ids() ids.ShortSet {
	ids := ids.ShortSet{}
	for _, id := range c.peerIDToID {
		ids.Add(id)
	}
	return ids
}

func (c *connections) ips() []utils.IPDesc {
	ips := make([]utils.IPDesc, 0, len(c.idToIP))
	for _, ip := range c.idToIP {
		ips = append(ips, ip)
	}
	return ips
}

func (c *connections) conns() ([]salticidae.PeerID, []ids.ShortID, []utils.IPDesc) {
	peers := make([]salticidae.PeerID, 0, len(c.idToPeerID))
	idList := make([]ids.ShortID, 0, len(c.idToPeerID))
	ips := make([]utils.IPDesc, 0, len(c.idToPeerID))
	for id, peer := range c.idToPeerID {
		idList = append(idList, ids.NewShortID(id))
		peers = append(peers, peer)
		ips = append(ips, c.idToIP[id])
	}
	return peers, idList, ips
}

func (c *connections) len() int { return len(c.idToPeerID) }

func toID(peer salticidae.PeerID) [32]byte {
	ds := salticidae.NewDataStream(false)

	peerInt := peer.AsUInt256()
	peerInt.Serialize(ds)

	size := ds.Size()
	dsb := ds.GetDataInPlace(size)

	idBytes := dsb.Get()
	id := [32]byte{}
	copy(id[:], idBytes)

	dsb.Release()
	ds.Free()
	return id
}

func toIPDesc(addr salticidae.NetAddr) utils.IPDesc {
	ip, err := ToIPDesc(addr)
	HandshakeNet.log.AssertNoError(err)
	return ip
}

// ToIPDesc converts an address to an IP
func ToIPDesc(addr salticidae.NetAddr) (utils.IPDesc, error) {
	ip := salticidae.FromBigEndianU32(addr.GetIP())
	port := salticidae.FromBigEndianU16(addr.GetPort())
	return utils.ToIPDesc(fmt.Sprintf("%d.%d.%d.%d:%d", byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip), port))
}
