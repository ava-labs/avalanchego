package settleledger_test

import (
	"encoding/binary"
	"math/big"
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"

	settleledger "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/settleledgerevidence"
)

type blockCtx struct{}

func (blockCtx) Number() *big.Int  { return big.NewInt(7) }
func (blockCtx) Timestamp() uint64 { return 7 }
func (blockCtx) GetPredicateResults(common.Hash, common.Address) set.Bits {
	return set.NewBits()
}

type env struct{ s contract.StateDB }

func (e env) GetStateDB() contract.StateDB             { return e.s }
func (env) GetBlockContext() contract.BlockContext     { return blockCtx{} }
func (env) GetSnowContext() *snow.Context              { return nil }
func (env) GetPrecompileEnv() vm.PrecompileEnvironment { return nil }
func (env) GetRules() precompileconfig.Rules           { return nil }

var engine = settleledger.ContractAddress

func sel(s string) []byte { return crypto.Keccak256([]byte(s))[:4] }

func word(v uint64) []byte       { var b [32]byte; binary.BigEndian.PutUint64(b[24:], v); return b[:] }
func aw(a common.Address) []byte { var b [32]byte; copy(b[12:], a[:]); return b[:] }
func cat(p ...[]byte) []byte {
	var o []byte
	for _, x := range p {
		o = append(o, x...)
	}
	return o
}

func idx(prefix byte, i int) common.Address {
	var a common.Address
	a[0] = prefix
	binary.BigEndian.PutUint32(a[16:20], uint32(i+1))
	return a
}

func call(t *testing.T, s contract.StateDB, input []byte) []byte {
	t.Helper()
	out, _, err := settleledger.SettleLedgerPrecompile.Run(env{s}, common.Address{}, engine, input, 1<<40, false)
	if err != nil {
		t.Fatalf("call %x: %v", input[:4], err)
	}
	return out
}

func packed(s []byte, body []byte) []byte {
	o := cat(s, word(32), word(uint64(len(body))), body)
	if p := len(body) % 32; p != 0 {
		o = append(o, make([]byte, 32-p)...)
	}
	return o
}

// world runs the plant's setup and a sequence of settle batches; it returns every
// batch's output and the final state root.
func world(t *testing.T, presize, v2, bulk bool, seed int64) ([][]byte, common.Hash) {
	settleledger.SetModes(presize, v2, bulk)
	defer settleledger.SetModes(false, false, false)
	sdb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	if err != nil {
		t.Fatal(err)
	}
	sdb.SetNonce(engine, 1)
	s := extstate.New(sdb)
	const tokens, parties, holders = 12, 30, 4
	rng := rand.New(rand.NewSource(seed))
	var setup [][]byte
	for i := 0; i <= tokens; i++ { // the cash token sits at index tokens
		a := idx(0xA0, i)
		if i == tokens {
			a = idx(0xC0, 0)
		}
		setup = append(setup, cat(sel("setTokenAt(uint256,address)"), word(uint64(i)), aw(a)))
	}
	for i := 0; i < parties; i++ {
		setup = append(setup, cat(sel("setPartyAt(uint256,address)"), word(uint64(i)), aw(idx(0xE0, i))))
		if i != 29 { // party 29 stays unbound
			setup = append(setup, cat(sel("bind(address,address)"), aw(idx(0xE0, i)), aw(idx(0xB0, i%holders))))
		}
	}
	for h := 0; h < holders; h++ {
		setup = append(setup, cat(sel("setHolderAllowed(address,bool)"), aw(idx(0xB0, h)), word(1)))
	}
	rng.Shuffle(len(setup), func(i, j int) { setup[i], setup[j] = setup[j], setup[i] }) // the loadgen sends them concurrently
	for _, in := range setup {
		call(t, s, in)
	}
	call(t, s, cat(sel("setCashToken(address)"), aw(idx(0xC0, 0))))
	var body []byte
	for p := 0; p < parties-1; p++ {
		for tk := 0; tk <= tokens; tk++ {
			if tk != tokens && rng.Intn(3) == 0 {
				continue // some positions never opened
			}
			body = append(body, byte(tk>>8), byte(tk), byte(p>>8), byte(p))
		}
	}
	for len(body) > 0 {
		n := min(len(body), 400*4)
		amt := word(1000 + uint64(rng.Intn(50)))
		call(t, s, cat(sel("creditPacked(uint256,bytes)"), amt, word(64), word(uint64(n)), body[:n]))
		body = body[n:]
	}
	sdb.Finalise(true)
	var outs [][]byte
	ref := uint64(0)
	for b := 0; b < 25; b++ {
		var rows []byte
		for r := 0; r < 400; r++ {
			var row [26]byte
			ref++
			if rng.Intn(20) == 0 && ref > 3 {
				binary.BigEndian.PutUint64(row[0:8], ref-uint64(1+rng.Intn(3))) // duplicates
			} else {
				binary.BigEndian.PutUint64(row[0:8], ref)
			}
			binary.BigEndian.PutUint16(row[8:10], uint16(rng.Intn(tokens)))
			binary.BigEndian.PutUint16(row[10:12], uint16(rng.Intn(parties)))
			binary.BigEndian.PutUint16(row[12:14], uint16(rng.Intn(parties)))
			binary.BigEndian.PutUint32(row[14:18], uint32(rng.Intn(400)))
			binary.BigEndian.PutUint64(row[18:26], uint64(rng.Intn(400)))
			rows = append(rows, row[:]...)
		}
		snap := s.Snapshot()
		outs = append(outs, call(t, s, packed(sel("settlePacked(bytes)"), rows)))
		if b%7 == 3 { // a reverted call: its writes must vanish
			s.RevertToSnapshot(snap)
			outs = outs[:len(outs)-1]
		}
		sdb.Finalise(true)
	}
	return outs, sdb.IntermediateRoot(true)
}

func settledSeq(t *testing.T, outs [][]byte) (n int) {
	for _, o := range outs {
		for _, b := range o[64:] {
			for ; b != 0; b &= b - 1 {
				n++
			}
		}
	}
	return n
}

// TestKeyspace2SameOutcomes: v1, v1 presized, v2 and v2 bulk settle exactly the same rows
// of the same batches; v2 and v2 bulk end on the same state root, v1 and v1 presized too.
func TestKeyspace2SameOutcomes(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		o1, r1 := world(t, false, false, false, seed)
		op, rp := world(t, true, false, false, seed)
		o2, r2 := world(t, true, true, false, seed)
		ob, rb := world(t, true, true, true, seed)
		if r1 != rp {
			t.Fatalf("seed %d: presize changed the root", seed)
		}
		if r2 != rb {
			t.Fatalf("seed %d: bulk changed the v2 root", seed)
		}
		if r1 == r2 {
			t.Fatalf("seed %d: v2 root equals v1 root (keyspace not used?)", seed)
		}
		for i := range o1 {
			for _, o := range [][][]byte{op, o2, ob} {
				if string(o[i]) != string(o1[i]) {
					t.Fatalf("seed %d batch %d: settled bitmap differs", seed, i)
				}
			}
		}
		if n := settledSeq(t, o1); n < 1000 {
			t.Fatalf("seed %d: only %d rows settled", seed, n)
		}
		t.Logf("seed %d: %d rows settled in %d batches", seed, settledSeq(t, o1), len(o1))
	}
	_ = uint256.NewInt
}
