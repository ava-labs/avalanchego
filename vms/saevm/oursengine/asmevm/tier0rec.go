//go:build rwtrace

package asmevm

import (
	"fmt"
	"path/filepath"

	"github.com/holiman/uint256"
)

// Per-tx facts for the faster firewood tier0 writer (patch tier0): the sequential window replay
// of ccReplay, plus per tx the consensus receipt, the output, the logs and the write set with old
// and new values, and per block a stand-in body. The write sets are gated against the fixture:
// every write's old value equals the previous new value of its key (or the prestate), each
// block's net change equals the block's diff, and the end state equals final_diff.

// T0Write kinds. Account fields are pseudo-keys; a code hash of zero means the account is absent.
const (
	T0Slot    = 0
	T0Balance = 1
	T0Nonce   = 2
	T0Code    = 3
	T0Multi   = 4 // the sticky coreth multicoin flag (0 or 1); coin balances are T0Slot writes keyed by coin id
)

// T0Write is one changed key of a tx (old != new). Values are 32 bytes big-endian.
type T0Write struct {
	Addr     Address
	Kind     byte
	Slot     Hash // T0Slot only
	Old, New Hash
}

// T0Tx is what one executed tx produced.
type T0Tx struct {
	Hash    Hash
	Receipt []byte // consensus encoding; nil until EncodeReceipt when lazy
	Output  []byte
	Logs    []Log
	Writes  []T0Write
	rc      Receipt // the receipt to encode (lazy)
	lazy    bool
}

// lazyTx is t0TxOf with the receipt left to EncodeReceipt.
func lazyTx(t *RTx, r *ExecResult, cum uint64, ws []T0Write) T0Tx {
	return T0Tx{Hash: t.Hash, Output: r.Output, Logs: r.Logs, Writes: ws,
		rc: Receipt{Type: t.Type, Success: r.Status == "success", CumGas: cum, Logs: r.Logs}, lazy: true}
}

// EncodeReceipt sets Receipt of a tx committed with Speculator.LazyReceipts (a no-op for any
// other tx). It may run on another goroutine than the committer, before anything reads Receipt.
func (t *T0Tx) EncodeReceipt() {
	if t.lazy {
		t.Receipt = EncodeReceipt(&t.rc)
		t.rc, t.lazy = Receipt{}, false
	}
}

// T0Block is what one executed block produced.
type T0Block struct {
	Number   uint64
	Hash     Hash
	Body     []byte    // stand-in: rlp of the fixture's header context and unsigned txs
	Pre, End []T0Write // pre-tx ops (EIP-4788) and the atomic end-of-block ops
	Txs      []T0Tx
	Codes    [][]byte // the code of every code hash a write sets (no code, empty code excluded), once each
}

// t0Codes fills tb.Codes from st at the end of the block: an account's code cannot change
// after it is set (SELFDESTRUCT deletes only accounts created in the same tx, whose writes
// net out), so the end-of-block code is the code each code hash write set.
func t0Codes(st *State, tb *T0Block) {
	empty := Hash(Keccak256(nil))
	seen := map[Hash]bool{}
	add := func(ws []T0Write) {
		for _, w := range ws {
			if w.Kind != T0Code || w.New == (Hash{}) || w.New == empty || seen[w.New] {
				continue
			}
			seen[w.New] = true
			tb.Codes = append(tb.Codes, st.Code(w.Addr))
		}
	}
	add(tb.Pre)
	for _, t := range tb.Txs {
		add(t.Writes)
	}
	add(tb.End)
}

type t0Acct struct {
	exists bool
	bal    uint256.Int
	nonce  uint64
	ch     Hash
	mc     bool
}

func t0AcctOf(st *State, addr Address) t0Acct {
	a := st.accounts[addr]
	// EIP-161: a touched account left empty is removed at the end of the tx (coreth: empty
	// means nonce 0, balance 0, no code and not multicoin); the interpreter keeps the object.
	if a == nil || a.empty() && !a.multicoin {
		return t0Acct{}
	}
	ch := a.codeHash
	if ch == (Hash{}) {
		ch = emptyCodeHash
	}
	return t0Acct{true, a.balance, a.nonce, ch, a.multicoin}
}

func t0Flag(b bool) (h Hash) {
	if b {
		h[31] = 1
	}
	return h
}

func t0U64(v uint64) Hash { return Hash(uint256.NewInt(v).Bytes32()) }

// t0AcctWrites appends the field changes of addr since shadow and moves shadow to the new value.
func t0AcctWrites(ws []T0Write, shadow map[Address]t0Acct, st *State, addr Address) []T0Write {
	o, n := shadow[addr], t0AcctOf(st, addr)
	if o == n {
		return ws
	}
	if o.bal != n.bal {
		ws = append(ws, T0Write{Addr: addr, Kind: T0Balance, Old: o.bal.Bytes32(), New: n.bal.Bytes32()})
	}
	if o.nonce != n.nonce {
		ws = append(ws, T0Write{Addr: addr, Kind: T0Nonce, Old: t0U64(o.nonce), New: t0U64(n.nonce)})
	}
	if o.ch != n.ch {
		ws = append(ws, T0Write{Addr: addr, Kind: T0Code, Old: o.ch, New: n.ch})
	}
	if o.mc != n.mc {
		ws = append(ws, T0Write{Addr: addr, Kind: T0Multi, Old: t0Flag(o.mc), New: t0Flag(n.mc)})
	}
	shadow[addr] = n
	return ws
}

func t0U256(s string) []byte { v := u256Of(s); return rlpString(v.Bytes()) }

// t0Body is the stand-in body: the fixture has no signatures and no full header.
func t0Body(b *ccBlock) []byte {
	h := rlpUint(b.Number)
	h = append(h, rlpString(hx(b.Hash))...)
	h = append(h, rlpString(hx(b.ParentHash))...)
	h = append(h, rlpUint(b.Timestamp)...)
	h = append(h, rlpUint(b.GasLimit)...)
	h = append(h, t0U256(b.BaseFee)...)
	h = append(h, rlpString(hx(b.Coinbase))...)
	var txs []byte
	for i := range b.Txs {
		t := &b.Txs[i]
		var to []byte
		if t.To != nil {
			to = hx(*t.To)
		}
		var al []byte
		for _, e := range t.AccessList {
			var keys []byte
			for _, k := range e.StorageKeys {
				keys = append(keys, rlpString(hx(k))...)
			}
			al = append(al, rlpList(append(rlpString(hx(e.Address)), rlpList(keys)...))...)
		}
		var f []byte
		if t.Type != 0 {
			f = rlpUint(b.ChainID)
		}
		f = append(f, rlpUint(t.Nonce)...)
		if t.Type == 2 {
			f = append(f, t0U256(t.GasTipCap)...)
			f = append(f, t0U256(t.GasFeeCap)...)
		} else {
			f = append(f, t0U256(t.GasPrice)...)
		}
		f = append(f, rlpUint(t.Gas)...)
		f = append(f, rlpString(to)...)
		f = append(f, t0U256(t.Value)...)
		f = append(f, rlpString(hx(t.Data))...)
		if t.Type != 0 {
			f = append(f, rlpList(al)...)
		}
		enc := rlpList(f)
		if t.Type != 0 {
			enc = rlpString(append([]byte{t.Type}, enc...))
		}
		txs = append(txs, enc...)
	}
	return rlpList(append(h, rlpList(txs)...))
}

type t0Key struct {
	addr Address
	kind byte
	slot Hash
}

// t0Gate replays the write sets on its own key map and checks them against the fixture.
type t0Gate struct {
	val, init map[t0Key]Hash
	start     map[t0Key]Hash // values at block start of the keys written in the block
	fails     []string
}

func (g *t0Gate) apply(where string, ws []T0Write) {
	for _, w := range ws {
		k := t0Key{w.Addr, w.Kind, w.Slot}
		if g.val[k] != w.Old {
			g.fails = append(g.fails, fmt.Sprintf("%s: write %x kind %d slot %x: old %x, previous new %x", where, w.Addr, w.Kind, w.Slot, w.Old, g.val[k]))
		}
		if _, ok := g.start[k]; !ok {
			g.start[k] = g.val[k]
		}
		g.val[k] = w.New
	}
}

// diff compares the running values with a fixture diff; ref holds the values of keys the diff
// does not list (block start, or the prestate for final_diff), and keys lists the keys to check
// against ref.
func (g *t0Gate) diff(where string, d []ccAcct, ref map[t0Key]Hash, keys map[t0Key]Hash) {
	listed := map[t0Key]bool{}
	chk := func(k t0Key, want Hash) {
		listed[k] = true
		if g.val[k] != want {
			g.fails = append(g.fails, fmt.Sprintf("%s: diff %x kind %d slot %x: want %x, write sets give %x", where, k.addr, k.kind, k.slot, want, g.val[k]))
		}
	}
	for _, a := range d {
		addr := addrOf(a.Address)
		var bal, nonce, ch Hash
		if a.Exists {
			b := u256Of(a.Balance)
			bal, nonce, ch = Hash(b.Bytes32()), t0U64(a.Nonce), hashOf(a.CodeHash)
		}
		chk(t0Key{addr, T0Balance, Hash{}}, bal)
		chk(t0Key{addr, T0Nonce, Hash{}}, nonce)
		chk(t0Key{addr, T0Code, Hash{}}, ch)
		s := hx(a.Storage)
		for i := 0; i+64 <= len(s); i += 64 {
			chk(t0Key{addr, T0Slot, T0Mask(Hash(s[i : i+32]))}, Hash(s[i+32:i+64]))
		}
	}
	for k := range keys {
		if !listed[k] && g.val[k] != ref[k] {
			g.fails = append(g.fails, fmt.Sprintf("%s: key %x kind %d slot %x changed to %x but the diff does not list it", where, k.addr, k.kind, k.slot, g.val[k]))
		}
	}
}

// RecordWindow replays the window at path in order like ReplayWindow and calls f with each
// block's facts. Fails holds receipt mismatches and write-set gate failures. roots=false is for
// windows without settlement headers (warp predicates then count as valid, roots unchecked).
func RecordWindow(path string, asm, roots bool, f func(*T0Block) error) (*WindowRun, error) {
	w, why, err := ccLoad(path)
	if err != nil {
		return nil, err
	}
	ccAssumePredicatesValid = !roots
	defer func() { ccAssumePredicatesValid = false }()
	st, acc, slots, err := ccPrestate(w)
	if err != nil {
		return nil, err
	}
	shadow := map[Address]t0Acct{}
	g := &t0Gate{val: map[t0Key]Hash{}, init: map[t0Key]Hash{}}
	for addr, a := range st.accounts {
		x := t0AcctOf(st, addr)
		shadow[addr] = x
		g.val[t0Key{addr, T0Balance, Hash{}}] = x.bal.Bytes32()
		g.val[t0Key{addr, T0Nonce, Hash{}}] = t0U64(x.nonce)
		g.val[t0Key{addr, T0Code, Hash{}}] = x.ch
		for k, v := range a.storage {
			g.val[t0Key{addr, T0Slot, T0Mask(k)}] = v.Bytes32()
		}
	}
	for k, v := range g.val {
		g.init[k] = v
	}
	touched := map[Address]struct{}{}
	st.RWHook = func(kind byte, addr Address, slot Hash, write bool) { touched[addr] = struct{}{} }
	defer func() { st.RWHook = nil }()
	curBlock, curTx := 0, 0
	where := func() string {
		b := &w.Blocks[curBlock]
		return fmt.Sprintf("block %d tx %d %s", b.Number, curTx, b.Txs[curTx].Hash)
	}
	run := &WindowRun{Name: filepath.Base(path), Blocks: len(w.Blocks), Unsupported: len(why)}
	rcpts := make([][]Receipt, len(w.Blocks))
	for bi := range w.Blocks {
		b := &w.Blocks[bi]
		blk, failed, err := ccBlockCtx(b)
		if err != nil {
			return nil, err
		}
		ccStrict(st, &blk, acc, slots, where)
		tb := &T0Block{Number: b.Number, Hash: hashOf(b.Hash), Body: t0Body(b)}
		g.start = map[t0Key]Hash{}
		for _, op := range b.PreOps {
			addr, slot, v := addrOf(op.Address), hashOf(*op.Slot), u256Of(*op.Value)
			if old := st.GetStorage(addr, slot); old != v {
				tb.Pre = append(tb.Pre, T0Write{Addr: addr, Kind: T0Slot, Slot: T0Mask(slot), Old: old.Bytes32(), New: v.Bytes32()})
			}
			st.SetStorage(addr, slot, v)
		}
		g.apply(fmt.Sprintf("block %d pre", b.Number), tb.Pre)
		coinbaseNew := st.accounts[blk.Coinbase] == nil
		cum := uint64(0)
		for ti := range b.Txs {
			curBlock, curTx = bi, ti
			jt := &b.Txs[ti]
			tx, txc := ccTxOf(jt, failed)
			clear(touched)
			r, gas := ccExec(st, &blk, b.FeeRules, &tx, txc, jt.FeeFloorGas, coinbaseNew, asm)
			touched[blk.Coinbase] = struct{}{}
			var ws []T0Write
			for addr := range touched {
				ws = t0AcctWrites(ws, shadow, st, addr)
			}
			for i := range st.orig.e {
				if e := &st.orig.e[i]; e.gen == st.orig.gen && !e.dead {
					var v uint256.Int
					if a := st.accounts[e.k.addr]; a != nil {
						v, _ = st.stored(a, &e.k.slot)
					}
					if v != e.v {
						ws = append(ws, T0Write{Addr: e.k.addr, Kind: T0Slot, Slot: T0Mask(e.k.slot), Old: e.v.Bytes32(), New: v.Bytes32()})
					}
				}
			}
			cum += gas
			rc := ccReceipt(jt, &r, cum)
			rcpts[bi] = append(rcpts[bi], rc)
			tb.Txs = append(tb.Txs, T0Tx{Hash: hashOf(jt.Hash), Receipt: EncodeReceipt(&rc), Output: r.Output, Logs: r.Logs, Writes: ws})
			g.apply(where(), ws)
			if bad := ccCheck(&r, gas, jt); len(bad) > 0 {
				run.Fails = append(run.Fails, fmt.Sprintf("%s: %v", where(), bad))
			}
			run.Gas += jt.Result.GasUsed
			run.Txs++
		}
		g.diff(fmt.Sprintf("block %d", b.Number), b.Diff, g.start, g.start)
		ccEndOps(st, b)
		for _, op := range b.EndOps {
			tb.End = t0AcctWrites(tb.End, shadow, st, addrOf(op.Address))
		}
		g.apply(fmt.Sprintf("block %d end", b.Number), tb.End)
		t0Codes(st, tb)
		if err := f(tb); err != nil {
			return nil, err
		}
	}
	g.diff("final", w.FinalDiff, g.init, g.val)
	run.Fails = append(run.Fails, g.fails...)
	run.Roots = &ccRoots{}
	if roots {
		if run.Roots, err = ccRootCheck(path, w, rcpts); err != nil {
			return nil, err
		}
	}
	return run, nil
}
