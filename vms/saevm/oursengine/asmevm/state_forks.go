package asmevm

import "github.com/holiman/uint256"

// MultiCoin is the multicoin balance of addr under key (a normalized coin id).
func (s *State) MultiCoin(addr Address, key Hash) uint256.Int {
	s.coinUse = true
	if a := s.acct(addr); a != nil {
		s.coin(addr, a, key)
		return a.mc[key]
	}
	return uint256.Int{}
}

// CoinBase is a base that holds multicoin balances (the tier0 store: overlay, then snapshot).
type CoinBase interface {
	Coin(addr Address, key Hash) (uint256.Int, error)
}

// coin reads coin id key of a multicoin account loaded from the base the first time it is
// used, as its committed value (not journaled). A base read failure panics (Chain reports it
// as the block's error).
func (s *State) coin(addr Address, a *account, key Hash) {
	if a.mcAsked == nil || a.mcAsked[key] {
		return
	}
	a.mcAsked[key] = true
	v, err := s.base.(CoinBase).Coin(addr, key)
	if err != nil {
		panic(err)
	}
	if !v.IsZero() {
		if a.mc == nil {
			a.mc = map[Hash]uint256.Int{}
		}
		a.mc[key] = v
	}
}

// SetMultiCoin sets a multicoin balance and the account's multicoin flag (coreth
// AddBalanceMultiCoin / SubBalanceMultiCoin), journaled.
func (s *State) SetMultiCoin(addr Address, key Hash, v uint256.Int) {
	s.coinUse = true
	a := s.getOrNew(addr)
	s.coin(addr, a, key)
	if a.mc == nil {
		a.mc = map[Hash]uint256.Int{}
	}
	a.sclean = false
	old, had := a.mc[key]
	flag := uint64(0)
	if a.multicoin {
		flag = 1
	}
	s.push(jentry{kind: jMultiCoin, addr: addr, acct: a, slot: key, u: old, had: had, n: flag})
	if v.IsZero() {
		delete(a.mc, key)
	} else {
		a.mc[key] = v
	}
	a.multicoin = true
}

func (s *State) destroyed(addr Address) bool {
	a := s.accounts[addr]
	return a != nil && a.destroyed
}

// BeginOps starts a group of state changes outside the EVM (a block's atomic txs).
func (s *State) BeginOps() { s.beginTx() }

// EndOps ends it like the end of a tx: EIP-161 removes every touched account left empty.
func (s *State) EndOps() { s.endTxLegacy() }

// endTxLegacy is geth's StateDB.Finalise before Cancun: every self-destructed account is
// removed with its storage, and (EIP-161) so is every account touched in the tx and left
// empty. The journal holds exactly the changes of the frames that did not revert.
func (s *State) endTxLegacy() {
	for _, t := range s.txAccts {
		if t.a.destroyed && s.accounts[t.addr] == t.a {
			s.forgetIndexed(t.addr)
			delete(s.accounts, t.addr)
		}
	}
	for i := range s.journal {
		e := &s.journal[i]
		switch e.kind {
		case jCreateAcct, jNonce, jBalance:
			if a := s.accounts[e.addr]; a != nil && a.empty() {
				s.forgetIndexed(e.addr)
				delete(s.accounts, e.addr)
			}
		}
	}
}

// AccountView is one account as the state root sees it.
type AccountView struct {
	Nonce        uint64
	Balance      uint256.Int
	CodeHash     Hash // zero: no code
	MultiCoin    bool
	StorageClean bool                 // storage and coins unchanged since MarkStorageClean
	Storage      map[Hash]uint256.Int // EVM slots (raw keys as the EVM used them)
	Coins        map[Hash]uint256.Int // multicoin balances (normalized coin ids)
}

// Account returns addr's account (ok false: absent). The maps are the State's own: read only.
func (s *State) Account(addr Address) (v AccountView, ok bool) {
	a := s.accounts[addr]
	if a == nil {
		return v, false
	}
	return AccountView{a.nonce, a.balance, a.codeHash, a.multicoin, a.sclean, a.storage, a.mc}, true
}

// MarkStorageClean records that addr's storage root was just computed (AccountView.StorageClean).
func (s *State) MarkStorageClean(addr Address) {
	if a := s.accounts[addr]; a != nil {
		a.sclean = true
	}
}

// Addresses calls f for every account.
func (s *State) Addresses(f func(addr Address)) {
	for addr := range s.accounts {
		f(addr)
	}
}

// SetAccount installs a genesis account.
func (s *State) SetAccount(addr Address, nonce uint64, balance uint256.Int, code []byte, storage map[Hash]uint256.Int) {
	a := newAccount()
	a.nonce, a.balance = nonce, balance
	if len(code) > 0 {
		a.code, a.codeHash = code, Keccak256(code)
	}
	for k, v := range storage {
		a.storage[k] = v
	}
	s.accounts[addr] = a
}

// MarkStorageDirty forces addr's storage root to be recomputed by the next root.
func (s *State) MarkStorageDirty(addr Address) {
	if a := s.accounts[addr]; a != nil {
		a.sclean = false
	}
}
