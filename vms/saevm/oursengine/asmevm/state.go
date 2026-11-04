package asmevm

import (
	"slices"

	"github.com/holiman/uint256"
)

// Log is an emitted event.
type Log struct {
	Address Address
	Topics  []Hash
	Data    []byte
}

type account struct {
	nonce    uint64
	balance  uint256.Int
	code     []byte
	codeHash Hash
	jd       bitvec   // cached jumpdest analysis of code; nil = not computed
	dec      *decoded // cached asm-core stream of code; nil = not computed
	// storage holds the live value per slot (State.orig the original values).
	storage map[Hash]uint256.Int
	bh      uint32 // patch sixgg: handle of this account's storage in State.base (0: none)
	// based: the base values of slots read so far (State.baseSlot); nil until the first read
	based     map[Hash]uint256.Int
	created   bool // created in this tx (EIP-6780 and empty-account rules)
	destroyed bool
	inTx      bool // listed in State.txAccts
	// mc holds the coreth multicoin balances (key = coin id with bit 0 of byte 0 set, a key no
	// EVM write uses: coreth normalizes EVM keys to bit 0 clear); multicoin is the account flag.
	mc        map[Hash]uint256.Int
	multicoin bool
	// mcAsked, non-nil for a multicoin account loaded from the base (a Loader): the coin ids
	// already read from it (State.coin); other balances are still below, in the base.
	mcAsked map[Hash]bool
	// sclean: storage and multicoin balances unchanged since MarkStorageClean (the zero value
	// is dirty, so a new account always is).
	sclean bool
}

func newAccount() *account {
	return &account{storage: map[Hash]uint256.Int{}}
}

func (a *account) empty() bool {
	return a.nonce == 0 && a.balance.IsZero() && len(a.code) == 0
}

// State is an in-memory world state with an undo journal for snapshot/revert,
// EIP-2929 access lists and EIP-1153 transient storage.
//
// Decision (Phase 1): accounts and storage use Go maps for correctness. The
// pointer-free open-addressing table with generation counters (gauge design)
// is a measured Phase 3 change, kept out until parity is proven.
type State struct {
	accounts map[Address]*account
	txAccts  []txAcct // accounts with per-tx fields set (created, destroyed)

	warmAddr  genSet[Address, struct{}]
	warmSlot  genSet[slotKey, struct{}]
	orig      genSet[slotKey, uint256.Int] // value at the start of the tx of each slot written in it (EIP-2200 original value)
	transient map[slotKey]uint256.Int

	refund uint64
	logs   []Log

	warmAll bool // test host: every storage slot is warm (revm DenseHost parity)

	stab  *slotTable                   // warm slots for the native SLOAD; nil: none
	ctab  *codeTable                   // accounts the native CALL may enter; nil: none
	kmemo *[keccakMemoSize]keccakEntry // KECCAK256 memo of 64-byte inputs (ASMEVM_KCACHE=persistent4096)
	vtab  *slotTable                   // every slot's current value for the native cold SLOAD; nil: none (IndexStorage)
	nj    *nativeJournal

	journal     []jentry
	frames      []*asmFrame // free asm frame records (Interpreter.frameAt)
	codeJournal []codeUndo

	// RWHook and OpHook (build tag rwtrace only): per-State access and op
	// traces for measurements (rw above; OpHook sees every Go-loop op).
	RWHook func(kind byte, addr Address, slot Hash, write bool)
	OpHook func(op OpCode, stack *Stack)

	// base (patch sixgg): storage below the per-account maps, which then hold only the
	// writes since the base (the overlay). nil: the maps hold the whole prestate.
	base StorageBase

	// OnMiss, when set, sees every read of an account absent from the map
	// (slot nil) or of a slot absent from an account's storage map (fixture
	// mode: the C-Chain window harness reports any key outside the prestate).
	OnMiss func(addr Address, slot *Hash)

	// FeeReads: what the ops run so far read that the fee moves (FeeReadBaseFee, FeeReadPrice),
	// reverted frames included; the caller clears it (the speculation's gas price repair).
	FeeReads uint8
	// coinUse: a multicoin balance or a native asset precompile was used since the caller
	// cleared it (the speculation cannot validate those reads: see xbSpec)
	coinUse bool
}

// FeeReads bits.
const (
	FeeReadBaseFee uint8 = 1 << iota // BASEFEE ran
	FeeReadPrice                     // GASPRICE ran, or BALANCE or SELFBALANCE read the tx origin's balance
)

// rw calls s.RWHook (build tag rwtrace only, see rwtrace.go) before every
// account-field and storage access: kind 'e' existence/emptiness, 'b' balance,
// 'n' nonce, 'c' code, 's' storage slot. Without the tag it compiles to nothing.
func (s *State) rw(kind byte, addr Address, slot Hash, write bool) {
	if rwOn && s.RWHook != nil {
		s.RWHook(kind, addr, slot, write)
	}
}

// Loader serves the state below this run's writes.
type Loader interface {
	StorageBase // Slot(h, slot) with the slot as the EVM sees it; HasAny(h)
	// Account returns the base account of addr (ok false: absent) with its code and the
	// storage handle Slot takes (0: no base storage).
	Account(addr Address) (ok bool, nonce uint64, balance uint256.Int, codeHash Hash, code []byte, h uint32, err error)
	// BaseSlotKeys calls f with keccak(slot) of every base slot of handle h (all non-zero)
	// until f returns false.
	BaseSlotKeys(h uint32, f func(sh Hash) bool) error
}

// StorageBase serves the storage values below the overlay (patch sixgg, E2 of the faster
// firewood sixgg brief): h is the account's handle from UseBase, the result is zero when absent.
type StorageBase interface {
	Slot(h uint32, slot *[32]byte) uint256.Int
	HasAny(h uint32) bool
}

// UseBase moves the storage of every loaded account to b: open gives each account's handle,
// the per-account maps are emptied (they become the overlay of later writes), and the native
// cold SLOAD value table is dropped (its slots now live in b; cold SLOADs go to the host).
// Setup only, outside any timer.
func (s *State) UseBase(b StorageBase, open func(addr Address) uint32) {
	s.base = b
	for addr, a := range s.accounts {
		a.bh, a.based = open(addr), nil
		clear(a.storage)
	}
	s.DropValueTable()
}

// DropValueTable turns the native cold SLOAD off for this State (cold CALL stays).
func (s *State) DropValueTable() {
	s.vtab = nil
	if s.stab != nil {
		s.stab.vtab = nil
		s.stab.cold = 0
	}
}

// EachAccount calls f for every loaded account with its storage map (setup only).
func (s *State) EachAccount(f func(addr Address, nonce uint64, balance *uint256.Int, codeHash *Hash, hasCode bool, storage map[Hash]uint256.Int)) {
	for addr, a := range s.accounts {
		f(addr, a.nonce, &a.balance, &a.codeHash, len(a.code) > 0, a.storage)
	}
}

// stored is the slot's current value: the account's map, then the base. had reports a map
// entry (an undo of a write without one deletes it, uncovering the base again).
func (s *State) stored(a *account, slot *Hash) (v uint256.Int, had bool) {
	if v, had = a.storage[*slot]; !had && a.bh != 0 {
		v = s.baseSlot(a, slot)
	}
	return
}

// baseSlot is the base value of a's slot, asked of s.base once per account: the base under a
// State holds still while the State lives (a Chain starts a new State when its Loader's view
// moves, at a cut; a store's view that moves under a kept State changes only keys the State
// itself wrote, which its maps hold), so the first answer serves every later read without the
// store's shared cache.
func (s *State) baseSlot(a *account, slot *Hash) uint256.Int {
	if v, ok := a.based[*slot]; ok {
		return v
	}
	k := [32]byte(*slot) // the interface call leaks its argument: a copy made here, on a base read only, keeps every caller's slot on its stack
	v := s.base.Slot(a.bh, &k)
	if a.based == nil {
		a.based = map[Hash]uint256.Int{}
	}
	a.based[*slot] = v
	return v
}

func (s *State) acct(addr Address) *account {
	a := s.accounts[addr]
	if a == nil && s.OnMiss != nil {
		s.OnMiss(addr, nil)
		a = s.accounts[addr] // patch tier0: OnMiss may load the account (Chain)
	}
	return a
}

type slotKey struct {
	addr Address
	slot Hash
}

func NewState() *State {
	return &State{
		accounts:  map[Address]*account{},
		warmAddr:  newGenSet[Address, struct{}](64),
		warmSlot:  newGenSet[slotKey, struct{}](64),
		orig:      newGenSet[slotKey, uint256.Int](64),
		transient: map[slotKey]uint256.Int{},
	}
}

func (s *State) getOrNew(addr Address) *account {
	a := s.acct(addr)
	if a == nil {
		a = newAccount()
		s.accounts[addr] = a
		s.push(jentry{kind: jCreateAcct, addr: addr})
	}
	return a
}

// --- accessors ---

func (s *State) Exists(addr Address) bool { s.rw('e', addr, Hash{}, false); return s.acct(addr) != nil }
func (s *State) Empty(addr Address) bool {
	s.rw('e', addr, Hash{}, false)
	a := s.acct(addr)
	return a == nil || a.empty()
}
func (s *State) Nonce(addr Address) uint64 {
	s.rw('n', addr, Hash{}, false)
	if a := s.acct(addr); a != nil {
		return a.nonce
	}
	return 0
}
func (s *State) Balance(addr Address) uint256.Int {
	s.rw('b', addr, Hash{}, false)
	if a := s.acct(addr); a != nil {
		return a.balance
	}
	return uint256.Int{}
}
func (s *State) Code(addr Address) []byte {
	s.rw('c', addr, Hash{}, false)
	if a := s.acct(addr); a != nil {
		return a.code
	}
	return nil
}

// codeAndJumpdests returns addr's code and its cached jumpdest analysis.
func (s *State) codeAndJumpdests(addr Address) ([]byte, bitvec) {
	s.rw('c', addr, Hash{}, false)
	a := s.acct(addr)
	if a == nil || len(a.code) == 0 {
		return nil, nil
	}
	if a.jd == nil {
		a.jd = codeBitmap(a.code)
	}
	return a.code, a.jd
}

var emptyCodeHash = Hash(Keccak256(nil))

// decodedFor returns the asm-core stream of code, cached on addr's account when
// code is that account's code (init code is decoded per run).
func (s *State) decodedFor(addr Address, code []byte) *decoded {
	if a := s.accounts[addr]; a != nil && len(code) > 0 && len(a.code) == len(code) && &a.code[0] == &code[0] {
		if a.dec == nil {
			h := a.codeHash
			if h == (Hash{}) {
				h = Keccak256(code)
			}
			a.dec = cachedDecode(h, code) // kept for the State's life (decodecache.go)
		}
		return a.dec
	}
	return predecodeHook(code)
}

func (s *State) CodeHash(addr Address) Hash {
	s.rw('c', addr, Hash{}, false)
	a := s.acct(addr)
	if a == nil || a.empty() {
		return Hash{}
	}
	if len(a.code) == 0 {
		return emptyCodeHash
	}
	return a.codeHash
}
func (s *State) CodeSize(addr Address) int { return len(s.Code(addr)) }

func (s *State) GetStorage(addr Address, slot Hash) uint256.Int {
	s.rw('s', addr, slot, false)
	if a := s.acct(addr); a != nil {
		v, ok := s.stored(a, &slot)
		if !ok && a.bh == 0 && s.OnMiss != nil {
			miss := slot // only this copy escapes: &slot would heap-allocate every call
			s.OnMiss(addr, &miss)
		}
		return v
	}
	return uint256.Int{}
}

// committedStorage is the slot's value at the start of the transaction.
func (s *State) committedStorage(addr Address, slot Hash) uint256.Int {
	s.rw('s', addr, slot, false)
	a := s.acct(addr)
	if a == nil {
		return uint256.Int{}
	}
	if e := s.orig.get(&slotKey{addr, slot}, slotHash(&addr, &slot)); e != nil {
		return e.v
	}
	v, _ := s.stored(a, &slot)
	return v
}

// --- journal ---

type jkind uint8

const (
	jCreateAcct jkind = iota
	jNonce
	jBalance
	jCode
	jStorage
	jCreated
	jWarmAddr
	jWarmSlot
	jTransient
	jRefund
	jLog
	jDestroyed
	jMultiCoin // u: previous balance under slot, had: present; n: previous multicoin flag (0/1)
)

// jentry is one undo record. Typed entries instead of closures: appending
// one allocates nothing once the journal slice has grown.
type jentry struct {
	kind jkind
	had  bool
	addr Address
	acct *account
	slot Hash
	u    uint256.Int // previous balance / storage / transient value
	n    uint64      // previous nonce / refund, or index into codeJournal
}

type codeUndo struct {
	code     []byte
	codeHash Hash
	jd       bitvec
	dec      *decoded
}

func (s *State) push(e jentry) { s.journal = append(s.journal, e) }

// jnew appends a zero entry and returns it: hot paths fill it in place
// instead of building a jentry on the stack and copying it.
func (s *State) jnew() *jentry {
	n := len(s.journal)
	if n == cap(s.journal) {
		s.journal = slices.Grow(s.journal, 1)
	}
	s.journal = s.journal[:n+1]
	e := &s.journal[n]
	*e = jentry{}
	return e
}

// --- mutators (journaled) ---

func (s *State) SetNonce(addr Address, n uint64) {
	s.rw('n', addr, Hash{}, true)
	a := s.getOrNew(addr)
	s.push(jentry{kind: jNonce, addr: addr, acct: a, n: a.nonce})
	a.nonce = n
}

func (s *State) AddBalance(addr Address, v *uint256.Int) {
	s.rw('b', addr, Hash{}, true)
	a := s.getOrNew(addr)
	s.push(jentry{kind: jBalance, addr: addr, acct: a, u: a.balance})
	a.balance.Add(&a.balance, v)
}

func (s *State) SubBalance(addr Address, v *uint256.Int) {
	s.rw('b', addr, Hash{}, true)
	a := s.getOrNew(addr)
	s.push(jentry{kind: jBalance, addr: addr, acct: a, u: a.balance})
	a.balance.Sub(&a.balance, v)
}

func (s *State) SetCode(addr Address, code []byte) {
	s.rw('c', addr, Hash{}, true)
	s.resetCodes()
	a := s.getOrNew(addr)
	s.codeJournal = append(s.codeJournal, codeUndo{a.code, a.codeHash, a.jd, a.dec})
	s.push(jentry{kind: jCode, acct: a, n: uint64(len(s.codeJournal) - 1)})
	a.code, a.jd, a.dec = code, nil, nil
	if len(code) == 0 {
		a.codeHash = Hash{}
	} else {
		a.codeHash = Keccak256(code)
	}
}

type txAcct struct {
	addr Address
	a    *account
}

// txTouch lists a so beginTx and the end of ExecTx visit only this tx's accounts.
func (s *State) txTouch(addr Address, a *account) {
	if !a.inTx {
		a.inTx = true
		s.txAccts = append(s.txAccts, txAcct{addr, a})
	}
}

func (s *State) SetStorage(addr Address, slot Hash, v uint256.Int) {
	s.rw('s', addr, slot, true)
	a := s.getOrNew(addr)
	old, had := s.stored(a, &slot)
	o, first := s.orig.slot(&slotKey{addr, slot}, slotHash(&addr, &slot))
	if first {
		o.v = old // first write this tx: remember the original
	}
	s.push(jentry{kind: jStorage, addr: addr, acct: a, slot: slot, u: old, had: had})
	a.storage[slot] = v
	a.sclean = false
	if s.vtab != nil {
		w := wordsOf(&addr)
		s.vtab.update(&w, new(uint256.Int).SetBytes32(slot[:]), &v)
	}
	if s.stab != nil { // SSTORE warmed the slot
		w := wordsOf(&addr)
		s.stab.put(&w, new(uint256.Int).SetBytes32(slot[:]), &v, &o.v)
	}
}

// setStorageFlushed is SetStorage for a native SSTORE record (flushNative):
// the account is known, and the core already wrote the slotTable entry.
// The old value comes from the value table when it holds a non-zero one
// (the table mirrors the storage maps, so the slot is in the map); only a
// zero needs the map, to tell a stored zero from an absent slot.
func (s *State) setStorageFlushed(a *account, addr Address, w *addrWords, key *uint256.Int, v *uint256.Int) {
	slot := Hash(key.Bytes32())
	var ve *slotEntry
	if s.vtab != nil {
		if e := s.vtab.find(w, key); e.gen == s.vtab.gen {
			ve = e
		}
	}
	var old uint256.Int
	had := true
	if ve != nil && !ve.val.IsZero() {
		old = ve.val
	} else {
		old, had = s.stored(a, &slot)
	}
	if o, first := s.orig.slot(&slotKey{addr, slot}, slotHash(&addr, &slot)); first {
		o.v = old
	}
	e := s.jnew()
	e.kind, e.addr, e.acct, e.slot, e.u, e.had = jStorage, addr, a, slot, old, had
	a.storage[slot] = *v
	a.sclean = false
	if ve != nil {
		ve.val = *v
	}
}

func (s *State) markCreated(addr Address) {
	a := s.getOrNew(addr)
	s.push(jentry{kind: jCreated, acct: a, had: a.created})
	a.created = true
	s.txTouch(addr, a)
}

func (s *State) markDestroyed(addr Address) {
	s.resetCodes()
	a := s.getOrNew(addr)
	s.push(jentry{kind: jDestroyed, acct: a, had: a.destroyed})
	a.destroyed = true
	s.txTouch(addr, a)
}

// hasStorage reports a non-zero slot (EIP-7610 collision rule).
func (s *State) hasStorage(addr Address) bool {
	if a := s.accounts[addr]; a != nil {
		for _, v := range a.storage {
			if !v.IsZero() {
				return true
			}
		}
		if a.bh == 0 {
			return false
		}
		if l, ok := s.base.(Loader); ok { // patch tier0: exact, a base slot counts unless the run zeroed it
			zeroed := map[Hash]bool{}
			for k := range a.storage { // every value left in the map is zero here
				m := k
				m[0] &^= 1 // coreth normalizeStateKey
				zeroed[Hash(Keccak256(m[:]))] = true
			}
			any := false
			if err := l.BaseSlotKeys(a.bh, func(sh Hash) bool { any = !zeroed[sh]; return !any }); err != nil {
				panic(err)
			}
			return any
		}
		// shortcut (sixgg layout arms only, never the tier0 path): ignores overlay writes that
		// zeroed every base slot; the window gate checks it.
		return s.base.HasAny(a.bh)
	}
	return false
}

// --- EIP-2929 access lists ---

// accessAddr marks addr warm; returns true if it was cold.
func (s *State) accessAddr(addr Address) bool {
	if !s.warmAddr.add(&addr, addrHash(&addr)) {
		return false
	}
	s.push(jentry{kind: jWarmAddr, addr: addr})
	s.noteWarm(addr)
	return true
}

// warmAtStart marks addr warm before the first snapshot of the transaction:
// no revert reaches below that snapshot, so it needs no journal entry.
func (s *State) warmAtStart(addr Address) {
	if s.warmAddr.add(&addr, addrHash(&addr)) {
		s.noteWarm(addr)
	}
}

// accessSlot marks (addr, slot) warm; returns true if it was cold.
func (s *State) accessSlot(addr Address, slot Hash) bool {
	if s.warmAll {
		return false
	}
	k := slotKey{addr, slot}
	if !s.warmSlot.add(&k, slotHash(&addr, &slot)) {
		return false
	}
	// A slot brings its address into the access list (geth accessList.AddSlot, journaled
	// before the slot). It matters only for NativeAssetCall's callee, which runs without
	// having been made warm (see EVM.coldSelf).
	if s.warmAddr.add(&addr, addrHash(&addr)) {
		s.push(jentry{kind: jWarmAddr, addr: addr})
		s.noteWarm(addr)
	}
	s.push(jentry{kind: jWarmSlot, addr: addr, slot: slot})
	if t := s.stab; t != nil { // cache the warm slot for the native SLOAD (and keep slotTable.cold true)
		var v uint256.Int
		ok := false
		a := s.accounts[addr]
		if a != nil {
			v, ok = s.stored(a, &slot)
			ok = ok || a.bh != 0
		}
		if !ok { // not loaded: a read must reach Go (strict reads), so no native cold SLOAD this tx
			t.cold = 0
		} else {
			orig := v
			if o := s.orig.get(&k, slotHash(&addr, &slot)); o != nil {
				orig = o.v
			}
			w := wordsOf(&addr)
			t.put(&w, new(uint256.Int).SetBytes32(slot[:]), &v, &orig)
		}
	}
	return true
}

func (s *State) slotWarm(addr Address, slot Hash) bool {
	return s.warmSlot.has(&slotKey{addr, slot}, slotHash(&addr, &slot))
}

// --- EIP-1153 transient storage ---

func (s *State) GetTransient(addr Address, slot Hash) uint256.Int {
	return s.transient[slotKey{addr, slot}]
}
func (s *State) SetTransient(addr Address, slot Hash, v uint256.Int) {
	k := slotKey{addr, slot}
	old, had := s.transient[k]
	s.push(jentry{kind: jTransient, addr: addr, slot: slot, u: old, had: had})
	s.transient[k] = v
}

// --- refunds and logs ---

func (s *State) addRefund(v uint64) {
	e := s.jnew()
	e.kind, e.n = jRefund, s.refund
	s.refund += v
}
func (s *State) subRefund(v uint64) {
	e := s.jnew()
	e.kind, e.n = jRefund, s.refund
	s.refund -= v
}

func (s *State) addLog(l Log) {
	s.jnew().kind = jLog
	s.logs = append(s.logs, l)
}

// --- snapshot / revert ---

func (s *State) snapshot() int { return len(s.journal) }

func (s *State) revert(snap int) {
	for i := len(s.journal) - 1; i >= snap; i-- {
		e := &s.journal[i]
		switch e.kind {
		case jCreateAcct:
			s.resetCodes()
			delete(s.accounts, e.addr)
		case jNonce:
			e.acct.nonce = e.n
		case jBalance:
			e.acct.balance = e.u
		case jCode:
			s.resetCodes()
			c := s.codeJournal[e.n]
			e.acct.code, e.acct.codeHash, e.acct.jd, e.acct.dec = c.code, c.codeHash, c.jd, c.dec
		case jStorage:
			if e.had {
				e.acct.storage[e.slot] = e.u
			} else {
				delete(e.acct.storage, e.slot)
			}
			e.acct.sclean = false
			// the native tables get the value before the write, e.u: with had false the slot
			// was not in the map, and e.u is its base value (State.stored), not zero
			if s.stab != nil {
				w := wordsOf(&e.addr)
				s.stab.update(&w, new(uint256.Int).SetBytes32(e.slot[:]), &e.u)
			}
			if s.vtab != nil {
				w := wordsOf(&e.addr)
				s.vtab.update(&w, new(uint256.Int).SetBytes32(e.slot[:]), &e.u)
			}
		case jCreated:
			e.acct.created = e.had
		case jWarmAddr:
			s.rw('c', e.addr, Hash{}, false) // a reverted frame's accesses still shaped the tx
			s.resetCodes()
			s.warmAddr.remove(&e.addr, addrHash(&e.addr))
		case jWarmSlot:
			s.rw('s', e.addr, e.slot, false)
			s.warmSlot.remove(&slotKey{e.addr, e.slot}, slotHash(&e.addr, &e.slot))
			if s.stab != nil {
				s.stab.reset()  // the slot is cold again
				s.stab.cold = 0 // and the other warm slots are gone from the table
			}
		case jTransient:
			if e.had {
				s.transient[slotKey{e.addr, e.slot}] = e.u
			} else {
				delete(s.transient, slotKey{e.addr, e.slot})
			}
		case jRefund:
			s.refund = e.n
		case jLog:
			s.logs = s.logs[:len(s.logs)-1]
		case jDestroyed:
			e.acct.destroyed = e.had
		case jMultiCoin:
			if e.had {
				e.acct.mc[e.slot] = e.u
			} else {
				delete(e.acct.mc, e.slot)
			}
			e.acct.multicoin = e.n == 1
			e.acct.sclean = false
		}
	}
	s.journal = s.journal[:snap]
}

// beginTx clears per-tx state: original storage values (recorded lazily on
// first write), created flags, access lists, transient storage, refund, journal.
func (s *State) beginTx() {
	for _, t := range s.txAccts {
		t.a.created, t.a.inTx = false, false
	}
	s.orig.clear()
	s.txAccts = s.txAccts[:0]
	s.warmAddr.clear()
	s.warmSlot.clear()
	if len(s.transient) > 0 {
		clear(s.transient)
	}
	s.refund = 0
	s.logs = nil
	if s.stab != nil {
		s.stab.reset()
		s.stab.cold = 0
		if s.vtab != nil && !s.warmAll {
			s.stab.cold = 1
		}
	}
	s.resetCodes()
	if s.ctab != nil && s.ctab.cidx != nil && !s.warmAll {
		s.ctab.cold = 1
	}
	s.journal = s.journal[:0]
	s.codeJournal = s.codeJournal[:0]
}

// Clone deep-copies the state for a fresh run. Code slices are shared: they are
// never mutated in place.
func (s *State) Clone() *State {
	c := NewState()
	for addr, a := range s.accounts {
		n := *a
		n.based = nil // the clone has no base
		n.storage = make(map[Hash]uint256.Int, len(a.storage))
		for k, v := range a.storage {
			n.storage[k] = v
		}
		n.created, n.destroyed, n.inTx = false, false, false // per-tx fields do not survive a clone (it has no txAccts)
		c.accounts[addr] = &n
	}
	c.warmAll = s.warmAll
	return c
}
