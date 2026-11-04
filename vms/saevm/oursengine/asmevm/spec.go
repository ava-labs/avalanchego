package asmevm

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/holiman/uint256"
)

// Spec (patch sixgg, E3b of the faster firewood sixgg brief) runs window blocks speculatively
// for their reads only: a Mirror is a State kept at the committed state by the blocks' recorded
// diffs (what the executor's write sets would give), and each speculative tx is reverted right
// after it runs, so every tx of a block sees the committed state, not its block's earlier txs.
type Spec struct {
	w    *ccWindow
	blks []BlockContext
	txs  [][]Tx
	txcs [][]TxContext
}

// LoadSpec decodes the window at path for speculation (setup only).
func LoadSpec(path string) (*Spec, error) {
	w, _, err := ccLoad(path)
	if err != nil {
		return nil, err
	}
	p := &Spec{w: w, blks: make([]BlockContext, len(w.Blocks)), txs: make([][]Tx, len(w.Blocks)), txcs: make([][]TxContext, len(w.Blocks))}
	for bi := range w.Blocks {
		b := &w.Blocks[bi]
		var failed map[Hash][]byte
		if p.blks[bi], failed, err = ccBlockCtx(b); err != nil {
			return nil, err
		}
		for ti := range b.Txs {
			tx, txc := ccTxOf(&b.Txs[ti], failed)
			p.txs[bi] = append(p.txs[bi], tx)
			p.txcs[bi] = append(p.txcs[bi], txc)
		}
	}
	return p, nil
}

// Blocks is the number of window blocks.
func (p *Spec) Blocks() int { return len(p.w.Blocks) }

// Mirror is one speculation worker's State.
type Mirror struct {
	p    *Spec
	st   *State
	next int // blocks before next are committed
	asm  bool
	// Txs and Panics count the speculative txs run and those that panicked (results discarded).
	Txs, Panics int
}

// NewMirror builds a mirror at the window's prestate; setup sees its State (UseBase).
func (p *Spec) NewMirror(asm bool, setup func(st *State)) (*Mirror, error) {
	st, _, _, err := ccPrestate(p.w)
	if err != nil {
		return nil, err
	}
	if setup != nil {
		setup(st)
	}
	return &Mirror{p: p, st: st, asm: asm}, nil
}

// Commit applies the recorded diffs of blocks [m.next, upto): the mirror is then at the state
// before block upto.
func (m *Mirror) Commit(upto int) {
	st := m.st
	for ; m.next < upto; m.next++ {
		for _, d := range m.p.w.Blocks[m.next].Diff {
			addr := addrOf(d.Address)
			if !d.Exists {
				st.forgetIndexed(addr)
				delete(st.accounts, addr)
				continue
			}
			a := st.accounts[addr]
			if a == nil {
				a = newAccount() // created in the window: no base storage
				st.accounts[addr] = a
			}
			a.balance = u256Of(d.Balance)
			a.nonce = d.Nonce
			if h := strings.ToLower(d.CodeHash); h != "" && h != "0x"+strings.Repeat("0", 64) && h != "0x"+ccHexHash(emptyCodeHash) && hashOf(h) != a.codeHash {
				if code, ok := m.p.w.Codes[h]; ok {
					st.forgetIndexed(addr)
					a.code, a.codeHash, a.jd, a.dec = hx(code), hashOf(h), nil, nil
				}
			}
			s := hx(d.Storage)
			for i := 0; i+64 <= len(s); i += 64 {
				var v uint256.Int
				v.SetBytes(s[i+32 : i+64])
				a.storage[Hash(s[i:i+32])] = v
			}
		}
	}
	st.resetCodes()
}

// Txs is the number of transactions of block bi.
func (p *Spec) Txs(bi int) int { return len(p.txs[bi]) }

// RunTx runs tx ti of block bi against the mirror and reverts it.
func (m *Mirror) RunTx(bi, ti int) {
	st := m.st
	defer func() {
		if r := recover(); r != nil {
			m.Panics++
			if m.Panics == 1 {
				fmt.Printf("spec: block %d tx %d panicked (discarded): %v\n", m.p.w.Blocks[bi].Number, ti, r)
			}
		}
		st.revert(0)
		st.beginTx()
	}()
	m.Txs++
	evm := NewEVM(m.p.blks[bi], m.p.txcs[bi][ti], st)
	evm.EnableAsm(m.asm)
	evm.ExecTx(&m.p.txs[bi][ti])
}

// ReplayWindowTxs is ReplayWindow for a window without settlement headers (patch sixgg: the
// 10k training window): every tx's status, gas, refund, output and logs are checked, the
// receipts roots are not (Roots stays empty).
func ReplayWindowTxs(path string, asm bool) (*WindowRun, error) {
	w, why, err := ccLoad(path)
	if err != nil {
		return nil, err
	}
	el, n, fails, _, err := ccReplay(w, asm)
	if err != nil {
		return nil, err
	}
	r := &WindowRun{Name: filepath.Base(path), Blocks: len(w.Blocks), Txs: n, Unsupported: len(why), Elapsed: el, Fails: fails, Roots: &ccRoots{},
		Allocs: ccAllocs, AllocBytes: ccAllocB, GCs: ccGCs}
	for _, b := range w.Blocks {
		for _, t := range b.Txs {
			r.Gas += t.Result.GasUsed
		}
	}
	return r, nil
}
