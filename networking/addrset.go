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
	Add(salticidae.NetAddr, ids.ShortID)

	GetIP(ids.ShortID) (salticidae.NetAddr, bool)
	GetID(salticidae.NetAddr) (ids.ShortID, bool)

	ContainsIP(salticidae.NetAddr) bool
	ContainsID(ids.ShortID) bool

	Remove(salticidae.NetAddr, ids.ShortID)
	RemoveIP(salticidae.NetAddr)
	RemoveID(ids.ShortID)

	Peers() []utils.IPDesc
	IPs() []salticidae.NetAddr
	IDs() ids.ShortSet
	Conns() ([]utils.IPDesc, []ids.ShortID)
	RawConns() ([]salticidae.NetAddr, []ids.ShortID)

	Len() int
}

// AddrCert implements the Connections interface
type AddrCert struct {
	// ip -> id
	ipToID map[uint64]ids.ShortID
	// id -> ip
	idToIP map[[20]byte]salticidae.NetAddr
	mux    sync.Mutex
}

// Add Assumes that addr is garbage collected normally
func (ac *AddrCert) Add(ip salticidae.NetAddr, id ids.ShortID) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	ac.add(ip, id)
}

// GetIP returns the ip mapped to the id that is provided if one exists.
func (ac *AddrCert) GetIP(id ids.ShortID) (salticidae.NetAddr, bool) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.getIP(id)
}

// GetID returns the id mapped to the ip that is provided if one exists.
func (ac *AddrCert) GetID(ip salticidae.NetAddr) (ids.ShortID, bool) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.getID(ip)
}

// ContainsIP returns true if the ip is contained in the connection pool
func (ac *AddrCert) ContainsIP(ip salticidae.NetAddr) bool {
	_, exists := ac.GetID(ip)
	return exists
}

// ContainsID returns true if the id is contained in the connection pool
func (ac *AddrCert) ContainsID(id ids.ShortID) bool {
	_, exists := ac.GetIP(id)
	return exists
}

// Remove ensures that no connection will have any mapping containing [ip] or
// [id].
func (ac *AddrCert) Remove(ip salticidae.NetAddr, id ids.ShortID) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	ac.remove(ip, id)
}

// RemoveIP ensures that no connection will have a mapping containing [ip]
func (ac *AddrCert) RemoveIP(ip salticidae.NetAddr) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	ac.removeIP(ip)
}

// RemoveID ensures that no connection will have a mapping containing [id]
func (ac *AddrCert) RemoveID(id ids.ShortID) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	ac.removeID(id)
}

// Peers returns the full list of ips contained in this connection pool.
func (ac *AddrCert) Peers() []utils.IPDesc {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.peers()
}

// IPs returns the full list of ips contained in this connection pool. This can
// be useful for gossiping a node's connections through the network.
func (ac *AddrCert) IPs() []salticidae.NetAddr {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.ips()
}

// IDs return the set of IDs that are mapping in this connection pool.
func (ac *AddrCert) IDs() ids.ShortSet {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.ids()
}

// Conns return the set of connections in this connection pool.
func (ac *AddrCert) Conns() ([]utils.IPDesc, []ids.ShortID) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.conns()
}

// RawConns return the set of connections in this connection pool.
func (ac *AddrCert) RawConns() ([]salticidae.NetAddr, []ids.ShortID) {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.rawConns()
}

// Len returns the number of elements in the map
func (ac *AddrCert) Len() int {
	ac.mux.Lock()
	defer ac.mux.Unlock()

	return ac.len()
}

func (ac *AddrCert) init() {
	if ac.ipToID == nil {
		ac.ipToID = make(map[uint64]ids.ShortID)
	}
	if ac.idToIP == nil {
		ac.idToIP = make(map[[20]byte]salticidae.NetAddr)
	}
}

func (ac *AddrCert) add(ip salticidae.NetAddr, id ids.ShortID) {
	ac.init()

	ac.removeIP(ip)
	ac.removeID(id)

	ac.ipToID[addrToID(ip)] = id
	ac.idToIP[id.Key()] = ip
}

func (ac *AddrCert) getIP(id ids.ShortID) (salticidae.NetAddr, bool) {
	ac.init()

	ip, exists := ac.idToIP[id.Key()]
	return ip, exists
}

func (ac *AddrCert) getID(ip salticidae.NetAddr) (ids.ShortID, bool) {
	ac.init()

	id, exists := ac.ipToID[addrToID(ip)]
	return id, exists
}

func (ac *AddrCert) remove(ip salticidae.NetAddr, id ids.ShortID) {
	ac.removeIP(ip)
	ac.removeID(id)
}

func (ac *AddrCert) removeIP(ip salticidae.NetAddr) {
	ac.init()

	ipID := addrToID(ip)
	if id, exists := ac.ipToID[ipID]; exists {
		delete(ac.ipToID, ipID)
		delete(ac.idToIP, id.Key())
	}
}

func (ac *AddrCert) removeID(id ids.ShortID) {
	ac.init()

	idKey := id.Key()
	if ip, exists := ac.idToIP[idKey]; exists {
		delete(ac.ipToID, addrToID(ip))
		delete(ac.idToIP, idKey)
	}
}

func (ac *AddrCert) peers() []utils.IPDesc {
	ac.init()

	ips := []utils.IPDesc(nil)
	for _, ip := range ac.idToIP {
		ips = append(ips, toIPDesc(ip))
	}
	return ips
}

func (ac *AddrCert) ips() []salticidae.NetAddr {
	ac.init()

	ips := []salticidae.NetAddr(nil)
	for _, ip := range ac.idToIP {
		ips = append(ips, ip)
	}
	return ips
}

func (ac *AddrCert) ids() ids.ShortSet {
	ac.init()

	ids := ids.ShortSet{}
	for _, id := range ac.ipToID {
		ids.Add(id)
	}
	return ids
}

func (ac *AddrCert) conns() ([]utils.IPDesc, []ids.ShortID) {
	ac.init()

	ipList := []utils.IPDesc(nil)
	idList := []ids.ShortID(nil)
	for id, ip := range ac.idToIP {
		ipList = append(ipList, toIPDesc(ip))
		idList = append(idList, ids.NewShortID(id))
	}
	return ipList, idList
}

func (ac *AddrCert) rawConns() ([]salticidae.NetAddr, []ids.ShortID) {
	ac.init()

	ipList := []salticidae.NetAddr(nil)
	idList := []ids.ShortID(nil)
	for id, ip := range ac.idToIP {
		ipList = append(ipList, ip)
		idList = append(idList, ids.NewShortID(id))
	}
	return ipList, idList
}

func (ac *AddrCert) len() int { return len(ac.ipToID) }

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

func addrToID(addr salticidae.NetAddr) uint64 {
	return uint64(addr.GetIP()) | (uint64(addr.GetPort()) << 32)
}
