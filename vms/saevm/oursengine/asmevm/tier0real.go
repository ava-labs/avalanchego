//go:build rwtrace

package asmevm

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/holiman/uint256"
)

// Real C-Chain execution for the faster firewood tier0 writer (patch tier0): blocks come from
// the node (decoded block bytes), state from a Loader (the pack base plus nothing else: every
// write since the base lives in this State's maps). Accounts are loaded on first access; an
// address is asked of the loader once per run, so an account deleted by a tx stays deleted.

// RTx is one transaction of a real block, decoded and with its sender recovered.
type RTx struct {
	Hash       Hash
	Type       uint8
	From       Address
	To         *Address
	Nonce, Gas uint64
	GasPrice   uint256.Int // effective price at the block's execution base fee
	Value      uint256.Int
	Data       []byte
	AccessList []AccessTuple
}

// RAcct is an absolute end-of-block account write (atomic txs).
type RAcct struct {
	Addr    Address
	Balance uint256.Int
	Nonce   uint64
}

// RBlock is one real block's execution input.
type RBlock struct {
	Number, Time, GasLimit uint64
	Hash, Parent           Hash
	Coinbase               Address
	BaseFee                uint256.Int // the SAE execution base fee, not the header's
	Extra                  []byte      // header extra: warp predicate results at offset 0
	Txs                    []RTx
	// Atomic applies the block's atomic txs after the EVM txs (st reads and writes balances
	// and nonces through the State API); nil: none.
	Atomic func(st *State) error
	// Fork, set for a block before SAE (Helicon): its EVM context as the fork rules build it
	// (forks.BlockContext: rules, difficulty, header base fee, blob fee) and its warp predicate
	// results by tx hash. Txs then run under the fork's own fee rules (RunTxReceipt), and the
	// RTx gas prices are the effective prices. nil: an SAE block.
	Fork *ForkCtx
}

// ForkCtx is the EVM context of a pre-SAE block (see RBlock.Fork).
type ForkCtx struct {
	Ctx        BlockContext
	WarpFailed map[Hash][]byte
	// Begin, when set, applies the state work before the block's txs (forks.BeginBlock:
	// precompile activations, the EIP-4788 system call) under the block's final context; its
	// writes are the block's pre-tx group.
	Begin func(st *State, blk BlockContext) error
}

// RTxOut is what one tx produced beyond the tier0 facts: the receipt fields the gate checks.
type RTxOut struct {
	Status  string // success | revert | halt
	GasUsed uint64 // receipt gas (fee rules applied)
	CumGas  uint64
}

// Chain executes real blocks in order on one State.
type Chain struct {
	// LazyReceipts: Exec leaves every receipt to T0Tx.EncodeReceipt (see Speculator.LazyReceipts)
	LazyReceipts bool
	st           *State
	l            Loader
	asm          bool
	av           *Avalanche
	chainID      uint64
	shadow       map[Address]t0Acct
	seen         map[Address]bool
	touched      map[Address]struct{}
	hashes       map[uint64]Hash       // the last 256 block hashes
	coins        map[t0Key]uint256.Int // committed multicoin balances the run changed (coin id keys)
	err          error                 // loader failure inside the EVM (raised as a panic, reported here)
	aq           chan aliasMsg         // the alias detector's goroutine (see aliasLoop), in commit order
}

// aliasMsg is the alias detector's input: the slots one tx touched (as used), a reset (a new
// tier0, as Chain.Reset), or a sync request answered with the count.
type aliasMsg struct {
	keys  []slotKey
	reset bool
	sync  chan int
}

// aliasLoop counts the slot pairs used in the run that coreth's key normalization merges
// (masked slot -> the raw slot first used, per tier0), off the executor thread.
func aliasLoop(q chan aliasMsg) {
	raw, n := map[t0Key]Hash{}, 0
	for m := range q {
		switch {
		case m.sync != nil:
			m.sync <- n
		case m.reset:
			raw = map[t0Key]Hash{}
		}
		for _, k := range m.keys {
			tk := t0Key{k.addr, T0Slot, T0Mask(k.slot)}
			if prev, ok := raw[tk]; !ok {
				raw[tk] = k.slot
			} else if prev != k.slot {
				n++
			}
		}
	}
}

// Aliased is the number of slot pairs used in the run that coreth's key normalization merges
// (every tx executed so far counted).
func (c *Chain) Aliased() int {
	q := make(chan int)
	c.aq <- aliasMsg{sync: q}
	return <-q
}

// C-Chain constants of the SAE era (as the fixtures' extractor saw them).
var (
	cchainFeeRules = ccFeeRules{Refunds: false, BaseFeeToCoinbase: true, RefundQuotient: RefundQuotientEIP3529}
)

// NewChain starts a run on an empty State over l. parentHash is the hash of the block before
// the first one executed (BLOCKHASH of it).
func NewChain(l Loader, asm bool, networkID uint32, blockchainID Hash, chainID uint64, first uint64, parentHash Hash) *Chain {
	c := &Chain{st: NewState(), l: l, asm: asm, chainID: chainID,
		av:     cchainAvalanche(networkID, blockchainID),
		shadow: map[Address]t0Acct{}, seen: map[Address]bool{}, touched: map[Address]struct{}{},
		hashes: map[uint64]Hash{first - 1: parentHash}, coins: map[t0Key]uint256.Int{}, aq: make(chan aliasMsg, 4096)}
	go aliasLoop(c.aq)
	c.st.base = l
	c.st.OnMiss = c.miss
	c.st.RWHook = func(kind byte, addr Address, slot Hash, write bool) { c.touched[addr] = struct{}{} }
	return c
}

// Reset drops the run's State (every account, slot and code loaded or written so far) and the
// write-capture bookkeeping, keeping the last 256 block hashes: the executor's memory stays
// bounded by one tier0. Call it between blocks once the Loader (the store) holds every write
// executed so far (at a cut, after the cut file is in the overlay); the next blocks load what
// they touch from it again. A Speculator on c must be replaced too (its replicas grow likewise).
// SetLoader makes l the Loader below the run (a store switched to a new base); call Reset
// right after it, at a cut: the State's storage handles belong to the old Loader.
func (c *Chain) SetLoader(l Loader) { c.l = l }

func (c *Chain) Reset() {
	c.st = NewState()
	c.st.base = c.l
	c.st.OnMiss = c.miss
	c.st.RWHook = func(kind byte, addr Address, slot Hash, write bool) { c.touched[addr] = struct{}{} }
	c.shadow, c.seen, c.touched = map[Address]t0Acct{}, map[Address]bool{}, map[Address]struct{}{}
	c.coins = map[t0Key]uint256.Int{}
	c.aq <- aliasMsg{reset: true}
}

// miss loads an account on its first access. Slot misses (accounts without base storage)
// read zero.
func (c *Chain) miss(addr Address, slot *Hash) {
	if slot != nil || c.seen[addr] {
		return
	}
	c.seen[addr] = true
	if err := loadAccount(c.st, c.l, addr); err != nil {
		c.err = err
		panic(err)
	}
	if c.st.accounts[addr] != nil {
		c.shadow[addr] = t0AcctOf(c.st, addr)
	}
}

// T0Mask is coreth's normalizeStateKey (bit 248 cleared): the state key of an EVM slot. Write
// sets carry state keys.
func T0Mask(slot Hash) Hash {
	slot[0] &^= 1
	return slot
}

var errHashMiss = errors.New("BLOCKHASH of a block before the run's parent")

// Exec runs one block and returns its tier0 facts and the receipt fields per tx.
func (c *Chain) Exec(b *RBlock) (tb *T0Block, outs []RTxOut, err error) {
	defer func() {
		if p := recover(); p != nil {
			if c.err != nil {
				err = fmt.Errorf("block %d: loader: %w", b.Number, c.err)
			} else {
				err = fmt.Errorf("block %d: panic: %v", b.Number, p)
			}
		}
	}()
	blk, failed, fr, err := c.execCtx(b, c.hashes)
	if err != nil {
		return nil, nil, err
	}
	tb = &T0Block{Number: b.Number, Hash: b.Hash}
	if b.Fork != nil && b.Fork.Begin != nil {
		c.st.beginTx() // drop the last tx's originals: only the hook's own changes are captured
		clear(c.touched)
		if err := b.Fork.Begin(c.st, blk); err != nil {
			return nil, nil, fmt.Errorf("block %d: begin: %w", b.Number, err)
		}
		tb.Pre = c.txWrites(b.Coinbase)
	}
	c.st.acct(b.Coinbase) // load it: ccExec reads the map directly
	coinbaseNew := c.st.accounts[b.Coinbase] == nil
	cum := uint64(0)
	for i := range b.Txs {
		t := &b.Txs[i]
		tx := t.tx()
		clear(c.touched)
		r, gas := ccExec(c.st, &blk, fr, &tx, TxContext{WarpFailed: failed[t.Hash]}, t.feeFloor(), coinbaseNew, c.asm)
		ws := c.txWrites(b.Coinbase)
		cum += gas
		if c.LazyReceipts {
			tb.Txs = append(tb.Txs, lazyTx(t, &r, cum, ws))
		} else {
			tb.Txs = append(tb.Txs, t0TxOf(t, &r, cum, ws))
		}
		outs = append(outs, RTxOut{Status: r.Status, GasUsed: gas, CumGas: cum})
	}
	if b.Atomic != nil {
		clear(c.touched)
		if err := b.Atomic(c.st); err != nil {
			return nil, nil, fmt.Errorf("block %d: atomic: %w", b.Number, err)
		}
		for addr := range c.touched {
			tb.End = t0AcctWrites(tb.End, c.shadow, c.st, addr)
		}
		tb.End = c.coinWrites(tb.End)
		c.st.beginTx()
	}
	t0Codes(c.st, tb)
	c.hashes[b.Number] = b.Hash
	delete(c.hashes, b.Number-256)
	return tb, outs, nil
}

// forkFeeRules: a block before SAE runs under its fork's own fee rules (forks.Replayer.Exec).
var forkFeeRules = ccFeeRules{Native: true}

// execCtx is what b's txs run under, on the committed State and on a speculation's replica
// alike: the EVM context (BLOCKHASH from hashes), the warp predicate results by tx hash and the
// fee rules. A block before SAE takes them from its fork (RBlock.Fork: the rules object
// forks.BlockContext selected for it, the header base fee), an SAE block from its SAE header.
func (c *Chain) execCtx(b *RBlock, hashes map[uint64]Hash) (blk BlockContext, failed map[Hash][]byte, fr ccFeeRules, err error) {
	if b.Fork != nil {
		blk = b.Fork.Ctx
		blk.ChainID, blk.Avalanche, blk.BlockHashes = c.chainID, &Avalanche{NetworkID: c.av.NetworkID, BlockchainID: c.av.BlockchainID}, hashes
		blk.OnHashMiss = func(n uint64) { panic(fmt.Errorf("%w: %d", errHashMiss, n)) }
		return blk, b.Fork.WarpFailed, forkFeeRules, nil
	}
	if failed, err = ParseWarpResults(b.Extra); err != nil {
		return blk, nil, fr, fmt.Errorf("block %d: %v", b.Number, err)
	}
	return c.blockCtx(b, hashes), failed, cchainFeeRules, nil
}

// blockCtx is the EVM context of b (BLOCKHASH from hashes).
func (c *Chain) blockCtx(b *RBlock, hashes map[uint64]Hash) BlockContext {
	blk := saeContext(c.av, c.chainID, &SAEHeader{Number: b.Number, Time: b.Time, GasLimit: b.GasLimit, Coinbase: b.Coinbase, BaseFee: b.BaseFee}, hashes)
	blk.OnHashMiss = func(n uint64) { panic(fmt.Errorf("%w: %d", errHashMiss, n)) }
	return blk
}

func (t *RTx) tx() Tx {
	return Tx{Caller: t.From, To: t.To, Data: t.Data, Value: t.Value, Gas: t.Gas, Nonce: t.Nonce, GasPrice: t.GasPrice, AccessList: t.AccessList}
}

// feeFloor is the SAE minimum gas a tx is charged: half its gas limit, rounded up.
func (t *RTx) feeFloor() uint64 { return (t.Gas + 1) / 2 }

func t0TxOf(t *RTx, r *ExecResult, cum uint64, ws []T0Write) T0Tx {
	rc := Receipt{Type: t.Type, Success: r.Status == "success", CumGas: cum, Logs: r.Logs}
	return T0Tx{Hash: t.Hash, Receipt: EncodeReceipt(&rc), Output: r.Output, Logs: r.Logs, Writes: ws}
}

// txWrites is the write set of the tx just run on c.st (before the next beginTx): the account
// fields of every address in c.touched and the coinbase that changed since the shadow, and
// every slot the tx changed. It counts the slot pairs coreth's key normalization merges.
func (c *Chain) txWrites(coinbase Address) []T0Write {
	c.touched[coinbase] = struct{}{}
	// sized once: an account rarely changes more than two fields in a tx (balance, nonce)
	ws := make([]T0Write, 0, 2*len(c.touched)+len(c.st.orig.idx))
	for addr := range c.touched {
		ws = t0AcctWrites(ws, c.shadow, c.st, addr)
	}
	for e := range c.st.orig.all() {
		var v uint256.Int
		if a := c.st.accounts[e.k.addr]; a != nil {
			v, _ = c.st.stored(a, &e.k.slot)
		}
		if v != e.v {
			ws = append(ws, T0Write{Addr: e.k.addr, Kind: T0Slot, Slot: T0Mask(e.k.slot), Old: e.v.Bytes32(), New: v.Bytes32()})
		}
	}
	var ks []slotKey // sent to the alias goroutine: a fresh slice, sized once
	if n := len(c.st.warmSlot.idx); n > 0 {
		ks = make([]slotKey, 0, n)
	}
	for e := range c.st.warmSlot.all() { // every slot the tx touched
		ks = append(ks, e.k)
	}
	c.aq <- aliasMsg{keys: ks}
	return c.coinWrites(ws)
}

// coinWrites appends the multicoin balance changes of the ops just run (the State's journal,
// before the next beginTx): one T0Slot write per changed (account, coin id), old = the
// committed value. A deleted account's coins go with it (readers wipe its storage on the code
// hash write to 0), so they are dropped from c.coins without writes.
func (c *Chain) coinWrites(ws []T0Write) []T0Write {
	var keys []t0Key
	for i := range c.st.journal {
		e := &c.st.journal[i]
		if e.kind != jMultiCoin {
			continue
		}
		k := t0Key{e.addr, T0Slot, e.slot}
		if _, ok := c.coins[k]; !ok {
			c.coins[k] = e.u // the first change of the run: its old value is the committed one
		}
		keys = append(keys, k)
	}
	for _, k := range keys {
		old, ok := c.coins[k]
		if !ok { // listed twice
			continue
		}
		var cur uint256.Int
		a := c.st.accounts[k.addr]
		if a == nil || t0AcctOf(c.st, k.addr) == (t0Acct{}) {
			delete(c.coins, k)
			continue
		}
		cur = a.mc[k.slot]
		if cur != old {
			ws = append(ws, T0Write{Addr: k.addr, Kind: T0Slot, Slot: k.slot, Old: old.Bytes32(), New: cur.Bytes32()})
		}
		c.coins[k] = cur
	}
	return ws
}

// AddHash gives BLOCKHASH the hash of block n (n before the run's parent: the node's last 256
// block hashes at start).
func (c *Chain) AddHash(n uint64, h Hash) { c.hashes[n] = h }

// State is the run's State (the Atomic callback and checks use it).
func (c *Chain) State() *State { return c.st }

// Rebase moves the run onto l between blocks inside a tier0 (a fold's new store taken at the
// tip): l must answer every key as the current Loader does. Unlike Reset the State keeps every
// account, slot and write of the open tier0; each loaded account's storage handle is asked of l
// again (0 stays 0: no base storage) and its code is copied, since it may alias the old store's
// mapping, which the fold closes after the switch. A Speculator on c must be replaced (its
// replicas hold the old handles).
func (c *Chain) Rebase(l Loader) error {
	for addr, a := range c.st.accounts {
		if a.bh != 0 {
			ok, _, _, _, _, h, err := l.Account(addr)
			if err != nil {
				return fmt.Errorf("rebase %x: %w", addr, err)
			}
			if !ok {
				h = 0
			}
			a.bh = h
		}
		if a.code != nil {
			a.code = bytes.Clone(a.code)
		}
	}
	c.l = l
	c.st.base = l
	return nil
}
