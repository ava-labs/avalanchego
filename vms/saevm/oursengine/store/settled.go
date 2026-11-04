package store

import (
	"sync"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
)

// lane is one reader lane's lock, alone on its cache line: the trie side's workers read the
// settled view on their own lanes, so their read locks do not share one cache line (mu's).
type lane struct {
	sync.RWMutex
	_ [40]byte
}

// lockAll write-locks mu and every lane (a writer of the settled view: accts, an account's
// versions, set).
func (s *State) lockAll() {
	s.mu.Lock()
	for i := range s.lanes {
		s.lanes[i].Lock()
	}
}

func (s *State) unlockAll() {
	for i := range s.lanes {
		s.lanes[i].Unlock()
	}
	s.mu.Unlock()
}

// SettledSlotAtLane is SettledSlotAt under reader lane l's lock (l mod 16).
func (s *State) SettledSlotAtLane(l int, addr [20]byte, sh [32]byte) (v [32]byte, found, wiped bool) {
	m := &s.lanes[l&15]
	m.RLock()
	defer m.RUnlock()
	return s.settledSlotAt(addr, sh)
}

// SettledSlotsAtLane is SettledSlotsAt under reader lane l's lock.
func (s *State) SettledSlotsAtLane(l int, addr [20]byte, shs [][32]byte, vs [][32]byte, found []bool) (wiped bool) {
	m := &s.lanes[l&15]
	m.RLock()
	defer m.RUnlock()
	return s.settledSlotsAt(addr, shs, vs, found)
}

// SettledAccountLane is SettledAccount under reader lane l's lock.
func (s *State) SettledAccountLane(l int, addr [20]byte) (nonce uint64, balance, codeHash [32]byte, multicoin, found bool) {
	m := &s.lanes[l&15]
	m.RLock()
	defer m.RUnlock()
	return s.settledAccount(addr)
}

// SettledSlot looks state key key of addr up in the settled view of the overlay only (no base
// read): found is false when the overlay does not hold it; wiped reports that addr was deleted
// since the base in the settled view (its base storage is gone). The trie side reads the base
// itself.
func (s *State) SettledSlot(addr [20]byte, key [32]byte) (v [32]byte, found, wiped bool) {
	return s.SettledSlotAt(addr, base.Keccak(key[:]))
}

// SettledSlotAt is SettledSlot for the state key whose keccak256 is sh.
func (s *State) SettledSlotAt(addr [20]byte, sh [32]byte) (v [32]byte, found, wiped bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settledSlotAt(addr, sh)
}

func (s *State) settledSlotAt(addr [20]byte, sh [32]byte) (v [32]byte, found, wiped bool) {
	ov := s.accts[addr]
	if ov == nil {
		return v, false, false
	}
	del, wiped := delAt(ov.vers, s.set)
	v, found = ov.runs.slotAt(&sh, s.set, del)
	return v, found, wiped
}

// SettledSlotsAt is SettledSlotAt for the keys shs of addr under one read lock and one account
// lookup (the trie gathers a cut's overlay keys this way, not with a lock per key): vs[i] and
// found[i] are shs[i]'s value and found; wiped is SettledSlotAt's.
func (s *State) SettledSlotsAt(addr [20]byte, shs [][32]byte, vs [][32]byte, found []bool) (wiped bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settledSlotsAt(addr, shs, vs, found)
}

func (s *State) settledSlotsAt(addr [20]byte, shs [][32]byte, vs [][32]byte, found []bool) (wiped bool) {
	ov := s.accts[addr]
	if ov == nil {
		clear(vs[:len(shs)])
		clear(found[:len(shs)])
		return false
	}
	del, wiped := delAt(ov.vers, s.set)
	for i := range shs {
		vs[i], found[i] = ov.runs.slotAt(&shs[i], s.set, del)
	}
	return wiped
}

// SettledAccount is addr's account in the settled view of the overlay only (no base read);
// found is false when the overlay does not hold it. codeHash 0: the account does not exist;
// keccak256(""): no code.
func (s *State) SettledAccount(addr [20]byte) (nonce uint64, balance, codeHash [32]byte, multicoin, found bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settledAccount(addr)
}

func (s *State) settledAccount(addr [20]byte) (nonce uint64, balance, codeHash [32]byte, multicoin, found bool) {
	ov := s.accts[addr]
	if ov == nil {
		return 0, [32]byte{}, [32]byte{}, false, false
	}
	a := at(ov.vers, s.set)
	if a == nil {
		return 0, [32]byte{}, [32]byte{}, false, false
	}
	return a.nonce, a.balance, a.codeHash, a.multicoin, true
}
