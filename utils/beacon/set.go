// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beacon

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Set = (*set)(nil)

	errDuplicateID = errors.New("duplicated ID")
	errDuplicateIP = errors.New("duplicated IP")

	errUnknownID = errors.New("unknown ID")
	errUnknownIP = errors.New("unknown IP")
)

type Set interface {
	Add(Beacon) error

	RemoveByID(ids.NodeID) error
	RemoveByIP(netip.AddrPort) error

	Len() int

	IDsArg() string
	IPsArg() string
}

type set struct {
	ids     map[ids.NodeID]int
	ips     map[netip.AddrPort]int
	beacons []Beacon
}

func NewSet() Set {
	return &set{
		ids: make(map[ids.NodeID]int),
		ips: make(map[netip.AddrPort]int),
	}
}

func (s *set) Add(b Beacon) error {
	id := b.ID()
	_, duplicateID := s.ids[id]
	if duplicateID {
		return errDuplicateID
	}

	ip := b.IP()
	_, duplicateIP := s.ips[ip]
	if duplicateIP {
		return errDuplicateIP
	}

	s.ids[id] = len(s.beacons)
	s.ips[ip] = len(s.beacons)
	s.beacons = append(s.beacons, b)
	return nil
}

func (s *set) RemoveByID(idToRemove ids.NodeID) error {
	indexToRemove, exists := s.ids[idToRemove]
	if !exists {
		return errUnknownID
	}
	toRemove := s.beacons[indexToRemove]
	ipToRemove := toRemove.IP()

	indexToMove := len(s.beacons) - 1
	toMove := s.beacons[indexToMove]
	idToMove := toMove.ID()
	ipToMove := toMove.IP()

	s.ids[idToMove] = indexToRemove
	s.ips[ipToMove] = indexToRemove
	s.beacons[indexToRemove] = toMove

	delete(s.ids, idToRemove)
	delete(s.ips, ipToRemove)
	s.beacons[indexToMove] = nil
	s.beacons = s.beacons[:indexToMove]
	return nil
}

func (s *set) RemoveByIP(ip netip.AddrPort) error {
	indexToRemove, exists := s.ips[ip]
	if !exists {
		return errUnknownIP
	}
	toRemove := s.beacons[indexToRemove]
	idToRemove := toRemove.ID()
	return s.RemoveByID(idToRemove)
}

func (s *set) Len() int {
	return len(s.beacons)
}

func (s *set) IDsArg() string {
	sb := strings.Builder{}
	if len(s.beacons) == 0 {
		return ""
	}
	b := s.beacons[0]
	_, _ = sb.WriteString(b.ID().String())
	for _, b := range s.beacons[1:] {
		_, _ = sb.WriteString(",")
		_, _ = sb.WriteString(b.ID().String())
	}
	return sb.String()
}

func (s *set) IPsArg() string {
	sb := strings.Builder{}
	if len(s.beacons) == 0 {
		return ""
	}
	b := s.beacons[0]
	_, _ = sb.WriteString(b.IP().String())
	for _, b := range s.beacons[1:] {
		_, _ = sb.WriteString(",")
		_, _ = sb.WriteString(b.IP().String())
	}
	return sb.String()
}
