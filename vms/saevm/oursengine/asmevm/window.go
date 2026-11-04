package asmevm

// Real Avalanche C-Chain windows (../../faster-evm/assets/cchain-fixtures/window-*.json.gz,
// format in that folder's README.md): loading, the timed ordered replay
// (ReplayWindow) and the after-the-timer checks (receipt fields and the SAE
// settlement receipts roots). The parity test (cchain_fixture_test.go) and
// cmd/bench share this code.

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/holiman/uint256"
)

const cchainFixturesDir = "../../faster-evm/assets/cchain-fixtures"

type ccAcct struct {
	Address  string `json:"address"`
	Exists   bool   `json:"exists"`
	Balance  string `json:"balance"`
	Nonce    uint64 `json:"nonce"`
	CodeHash string `json:"code_hash"`
	Storage  string `json:"storage"`
}

type ccLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type ccTx struct {
	Index      int     `json:"index"`
	Hash       string  `json:"hash"`
	Type       uint8   `json:"type"`
	From       string  `json:"from"`
	To         *string `json:"to"`
	Nonce      uint64  `json:"nonce"`
	Gas        uint64  `json:"gas"`
	GasPrice   string  `json:"gas_price"`
	GasFeeCap  string  `json:"gas_fee_cap"` // patch tier0: stand-in body encoding
	GasTipCap  string  `json:"gas_tip_cap"`
	Value      string  `json:"value"`
	Data       string  `json:"data"`
	AccessList []struct {
		Address     string   `json:"address"`
		StorageKeys []string `json:"storage_keys"`
	} `json:"access_list"`
	BlobHashes         []string `json:"blob_hashes"`
	FeeFloorGas        uint64   `json:"fee_floor_gas"`
	InvokedPrecompiles []string `json:"invoked_precompiles"`
	Result             struct {
		Status        string  `json:"status"`
		GasUsed       uint64  `json:"gas_used"`
		GasGross      uint64  `json:"gas_gross"`
		RefundCounter uint64  `json:"refund_counter"`
		Output        string  `json:"output"`
		Logs          []ccLog `json:"logs"`
	} `json:"result"`
	Flags []string `json:"flags"`
}

type ccWrite struct {
	Address string  `json:"address"`
	Slot    *string `json:"slot"`
	Value   *string `json:"value"`
	Balance string  `json:"balance"`
	Nonce   *uint64 `json:"nonce"`
}

type ccRules struct {
	IsCancun bool
	IsPrague bool
	IsVerkle bool
}

type ccFeeRules struct {
	Refunds           bool   `json:"refunds"`
	BaseFeeToCoinbase bool   `json:"base_fee_to_coinbase"`
	RefundQuotient    uint64 `json:"refund_quotient"`
	// Native: the EVM's own fee rules (a block before SAE, RBlock.Fork): the tx runs as
	// RunTxReceipt and its receipt gas is RunTxReceipt's; the other fields are not used.
	Native bool `json:"-"`
	MinGas struct {
		Policy  string `json:"policy"`
		Divisor uint64 `json:"divisor"`
	} `json:"min_gas_consumption"`
}

type ccBlock struct {
	Number        uint64            `json:"number"`
	Hash          string            `json:"hash"`
	ParentHash    string            `json:"parent_hash"`
	Active        []string          `json:"active_precompiles"`
	Timestamp     uint64            `json:"timestamp"`
	GasLimit      uint64            `json:"gas_limit"`
	BaseFee       string            `json:"base_fee"`
	Coinbase      string            `json:"coinbase"`
	Difficulty    string            `json:"difficulty"`
	Prevrandao    *string           `json:"prevrandao"`
	BlobBaseFee   *string           `json:"blob_base_fee"`
	ChainID       uint64            `json:"chain_id"`
	Rules         ccRules           `json:"rules"`
	FeeRules      ccFeeRules        `json:"fee_rules"`
	SpecialActive []string          `json:"special_precompiles_active"`
	BlockHashes   map[string]string `json:"block_hashes"`
	PreOps        []ccWrite         `json:"pre_ops"`
	Txs           []ccTx            `json:"txs"`
	EndOps        []ccWrite         `json:"end_ops"`
	Diff          []ccAcct          `json:"diff"`
}

type ccWindow struct {
	Format      string            `json:"format"`
	ChainConfig map[string]any    `json:"chain_config"`
	Stats       map[string]any    `json:"stats"`
	First       uint64            `json:"first_block"`
	Last        uint64            `json:"last_block"`
	Codes       map[string]string `json:"codes"`
	Prestate    []ccAcct          `json:"prestate"`
	Blocks      []ccBlock         `json:"blocks"`
	FinalDiff   []ccAcct          `json:"final_diff"`
}

// ccModelled says this EVM implements the precompile at p on C-Chain.
func ccModelled(p Address) bool {
	if _, ok := precompile(p); ok {
		return true
	}
	_, ok := (&EVM{Block: BlockContext{Avalanche: &Avalanche{}}}).special(p)
	return ok
}

// ccLoad decodes a window and returns the reasons it is outside what this
// EVM models (empty when supported).
func ccLoad(path string) (*ccWindow, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, err
	}
	var w ccWindow
	if err := json.NewDecoder(gz).Decode(&w); err != nil {
		return nil, nil, err
	}
	if w.Format != "cchain-window/v1" {
		return nil, nil, fmt.Errorf("format %q", w.Format)
	}
	if p, _ := w.Stats["promotable"].(bool); !p {
		return nil, nil, fmt.Errorf("not promotable (extractor replay errors): %v %v", w.Stats["replay_uncaptured_reads"], w.Stats["replay_mismatches"])
	}
	if w.ChainConfig == nil {
		return nil, nil, fmt.Errorf("no chain_config")
	}
	var why []string
	for bi := range w.Blocks {
		b := &w.Blocks[bi]
		if id, _ := w.ChainConfig["chainId"].(float64); uint64(id) != b.ChainID {
			why = append(why, fmt.Sprintf("block %d: chain id %d vs chain_config %v", b.Number, b.ChainID, w.ChainConfig["chainId"]))
		}
		if !b.Rules.IsCancun || b.Rules.IsPrague || b.Rules.IsVerkle {
			why = append(why, fmt.Sprintf("block %d: fork rules %+v (supported: Cancun without Prague)", b.Number, b.Rules))
		}
		switch fr := b.FeeRules; {
		case fr.RefundQuotient != RefundQuotientEIP3529:
			why = append(why, fmt.Sprintf("block %d: refund quotient %d", b.Number, fr.RefundQuotient))
		case fr.MinGas.Policy != "none" && !(fr.MinGas.Policy == "ceil_div" && fr.MinGas.Divisor > 0):
			why = append(why, fmt.Sprintf("block %d: minimum gas policy %q", b.Number, fr.MinGas.Policy))
		}
		for _, tx := range b.Txs {
			for _, a := range tx.InvokedPrecompiles {
				if p := addrOf(a); !ccModelled(p) {
					why = append(why, fmt.Sprintf("block %d tx %d %s: invokes special precompile %s", b.Number, tx.Index, tx.Hash, a))
				}
			}
			if tx.Type > 2 {
				why = append(why, fmt.Sprintf("block %d tx %d %s: tx type %d", b.Number, tx.Index, tx.Hash, tx.Type))
			}
			for _, f := range tx.Flags {
				if r, ok := strings.CutPrefix(f, "avalanche_reserved_address:"); f == "blob_opcode" || ok && !ccModelled(addrOf(r)) {
					why = append(why, fmt.Sprintf("block %d tx %d %s: %s", b.Number, tx.Index, tx.Hash, f))
				}
			}
		}
	}
	return &w, why, nil
}

type ccSlotKey struct {
	a Address
	k Hash
}

func ccHexHash(h Hash) string { return hex.EncodeToString(h[:]) }

func ccLogsJSON(l []Log) string {
	out := []ccLog{}
	for _, x := range l {
		c := ccLog{Address: "0x" + hex.EncodeToString(x.Address[:]), Topics: []string{}, Data: "0x" + hex.EncodeToString(x.Data)}
		for _, t := range x.Topics {
			c.Topics = append(c.Topics, "0x"+ccHexHash(t))
		}
		out = append(out, c)
	}
	return canon(out)
}

// ccStrict makes every account, slot and BLOCKHASH read outside the
// prestate fatal: a missing key is a harness or extractor bug, never a zero.
// where names the running block and tx; the panic stack names the opcode.
func ccStrict(st *State, blk *BlockContext, acc map[Address]bool, slots map[ccSlotKey]bool, where func() string) {
	st.OnMiss = func(a Address, slot *Hash) {
		switch {
		case slot == nil && !acc[a]:
			panic(fmt.Sprintf("strict read: %s: account 0x%x is not in the prestate", where(), a[:]))
		case slot != nil && !slots[ccSlotKey{a, *slot}]:
			panic(fmt.Sprintf("strict read: %s: slot 0x%x 0x%x is not in the prestate", where(), a[:], slot[:]))
		}
	}
	blk.OnHashMiss = func(n uint64) { panic(fmt.Sprintf("strict read: %s: BLOCKHASH %d is not in the window", where(), n)) }
}

// ccPrestate builds the window's one initial State and the prestate key sets.
func ccPrestate(w *ccWindow) (*State, map[Address]bool, map[ccSlotKey]bool, error) {
	allowedAcc := map[Address]bool{}
	allowedSlot := map[ccSlotKey]bool{}
	var jc jCase
	for _, a := range w.Prestate {
		addr := addrOf(a.Address)
		allowedAcc[addr] = true
		st := hx(a.Storage)
		if len(st)%64 != 0 {
			return nil, nil, nil, fmt.Errorf("%s: storage blob length %d", a.Address, len(st))
		}
		ja := jAccount{Address: a.Address, Balance: a.Balance, Nonce: a.Nonce, Storage: map[string]string{}}
		for i := 0; i < len(st); i += 64 {
			k, v := Hash(st[i:i+32]), st[i+32:i+64]
			allowedSlot[ccSlotKey{addr, k}] = true
			if !a.Exists && !isZero(v) {
				return nil, nil, nil, fmt.Errorf("absent account %s has a non-zero slot", a.Address)
			}
			ja.Storage["0x"+ccHexHash(k)] = "0x" + hex.EncodeToString(v)
		}
		if h := strings.ToLower(a.CodeHash); a.Exists && h != "0x"+strings.Repeat("0", 64) && h != "0x"+ccHexHash(emptyCodeHash) {
			code, ok := w.Codes[h]
			if !ok {
				return nil, nil, nil, fmt.Errorf("%s: code %s not in codes", a.Address, h)
			}
			ja.Code = code
		}
		if a.Exists {
			jc.Accounts = append(jc.Accounts, ja)
		}
	}
	return buildState(jc), allowedAcc, allowedSlot, nil
}

// ccAssumePredicatesValid lets a profiling replay run blocks whose header is
// not in the sidecar: their warp predicates count as valid (never for a gate).
var ccAssumePredicatesValid bool

// ccBlockCtx is the execution context of one window block.
// It returns the warp failed-predicate bitsets of the block's header too.
func ccBlockCtx(b *ccBlock) (BlockContext, map[Hash][]byte, error) {
	av, err := ccAvalancheOf(b)
	if err != nil {
		return BlockContext{}, nil, err
	}
	s, err := ccSettlementLoad()
	if err != nil {
		return BlockContext{}, nil, err
	}
	var failed map[Hash][]byte
	if h := s.Headers[strings.ToLower(b.Hash)]; h != nil {
		if failed, err = ParseWarpResults(hx(h.ExtraData)); err != nil { // Helicon: predicate results at extraData offset 0
			return BlockContext{}, nil, fmt.Errorf("block %d: %v", b.Number, err)
		}
	} else {
		for _, t := range b.Txs { // without the header, a warp predicate's validity is unknown
			for _, e := range t.AccessList {
				if addrOf(e.Address) == warpAddr && !ccAssumePredicatesValid {
					return BlockContext{}, nil, fmt.Errorf("block %d: header %s missing and tx %s has a warp predicate", b.Number, b.Hash, t.Hash)
				}
			}
		}
	}
	blk := BlockContext{
		ChainID: b.ChainID, Avalanche: av,
		Coinbase: addrOf(b.Coinbase), GasLimit: b.GasLimit, BlockNumber: b.Number, Time: b.Timestamp,
		Difficulty: u256Of(b.Difficulty), BaseFee: u256Of(b.BaseFee), BlockHashes: map[uint64]Hash{},
	}
	if b.BlobBaseFee != nil {
		blk.BlobBaseFee = u256Of(*b.BlobBaseFee)
	}
	if b.Prevrandao != nil {
		blk.Random = hashOf(*b.Prevrandao)
	} else {
		blk.Random = Hash(blk.Difficulty.Bytes32()) // pre-merge rules: 0x44 returns DIFFICULTY
	}
	for n, h := range b.BlockHashes {
		v, err := strconv.ParseUint(n, 10, 64)
		if err != nil {
			return blk, nil, err
		}
		blk.BlockHashes[v] = hashOf(h)
	}
	return blk, failed, nil
}

// ccNetworkFile records the network id and the C-Chain blockchain id.
const ccNetworkFile = "../../faster-evm/comparison-20261018/cchain-receipt-headers/network-context.json"

var ccNet *Avalanche

// ccAvalancheOf is the block's Avalanche execution context.
func ccAvalancheOf(b *ccBlock) (*Avalanche, error) {
	if ccNet == nil {
		raw, err := os.ReadFile(ccNetworkFile)
		if err != nil {
			return nil, err
		}
		var nc struct {
			Responses []struct {
				Result struct {
					NetworkID    string `json:"networkID"`
					BlockchainID string `json:"blockchainID"`
				} `json:"result"`
			} `json:"responses"`
		}
		if err := json.Unmarshal(raw, &nc); err != nil {
			return nil, err
		}
		a := &Avalanche{}
		for _, r := range nc.Responses {
			if r.Result.NetworkID != "" {
				n, err := strconv.ParseUint(r.Result.NetworkID, 10, 32)
				if err != nil {
					return nil, err
				}
				a.NetworkID = uint32(n)
			}
			if r.Result.BlockchainID != "" {
				id, err := cb58Decode(r.Result.BlockchainID)
				if err != nil || len(id) != 32 {
					return nil, fmt.Errorf("blockchain id %q: %v", r.Result.BlockchainID, err)
				}
				a.BlockchainID = Hash(id)
			}
		}
		if a.NetworkID == 0 || a.BlockchainID == (Hash{}) {
			return nil, fmt.Errorf("%s: no network or blockchain id", ccNetworkFile)
		}
		ccNet = a
	}
	av := *ccNet
	for _, p := range b.Active {
		av.Warm = append(av.Warm, addrOf(p))
	}
	return &av, nil
}

// cb58Decode is Avalanche's base58 with a 4-byte sha256 checksum suffix.
func cb58Decode(s string) ([]byte, error) {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int)
	for _, c := range s {
		i := strings.IndexRune(alphabet, c)
		if i < 0 {
			return nil, fmt.Errorf("bad base58 digit %q", c)
		}
		n.Mul(n, big.NewInt(58)).Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	for _, c := range s { // leading '1' digits are zero bytes
		if c != '1' {
			break
		}
		b = append([]byte{0}, b...)
	}
	if len(b) < 4 {
		return nil, fmt.Errorf("short cb58")
	}
	body, sum := b[:len(b)-4], b[len(b)-4:]
	if h := sha256.Sum256(body); string(h[28:]) != string(sum) {
		return nil, fmt.Errorf("cb58 checksum")
	}
	return body, nil
}

// ccTxOf decodes one window transaction.
func ccTxOf(jt *ccTx, failed map[Hash][]byte) (Tx, TxContext) {
	tx := Tx{
		Caller: addrOf(jt.From), Data: hx(jt.Data), Value: u256Of(jt.Value), Gas: jt.Gas,
		Nonce: jt.Nonce, GasPrice: u256Of(jt.GasPrice),
	}
	if jt.To != nil {
		to := addrOf(*jt.To)
		tx.To = &to
	}
	for _, e := range jt.AccessList {
		at := AccessTuple{Address: addrOf(e.Address)}
		for _, k := range e.StorageKeys {
			at.Keys = append(at.Keys, hashOf(k))
		}
		tx.AccessList = append(tx.AccessList, at)
	}
	txc := TxContext{WarpFailed: failed[hashOf(jt.Hash)]}
	for _, h := range jt.BlobHashes {
		txc.BlobHashes = append(txc.BlobHashes, hashOf(h))
	}
	return tx, txc
}

// ccExec runs one transaction and applies the chain fee rules from this
// execution's own gas and refund; it returns the result and the receipt gas.
// coinbaseNew says the coinbase did not exist at block start.
func ccExec(st *State, blk *BlockContext, fr ccFeeRules, tx *Tx, txc TxContext, feeFloor uint64, coinbaseNew, asm bool) (ExecResult, uint64) {
	evm := NewEVM(*blk, txc, st)
	evm.EnableAsm(asm)
	r := evm.RunTxReceipt(tx)
	if fr.Native {
		return r, r.GasUsed
	}
	chainGas := r.GasGross
	if fr.Refunds {
		chainGas -= min(r.GasRefund, r.GasGross/fr.RefundQuotient)
	}
	chainGas = max(chainGas, feeFloor)
	ourCharge := new(uint256.Int).Mul(&tx.GasPrice, uint256.NewInt(r.GasUsed))
	chainCharge := new(uint256.Int).Mul(&tx.GasPrice, uint256.NewInt(chainGas))
	st.AddBalance(tx.Caller, ourCharge)
	st.SubBalance(tx.Caller, chainCharge)
	tip := new(uint256.Int).Sub(&tx.GasPrice, &blk.BaseFee)
	st.SubBalance(blk.Coinbase, new(uint256.Int).Mul(tip, uint256.NewInt(r.GasUsed)))
	paid := new(uint256.Int).Mul(tip, uint256.NewInt(chainGas))
	if fr.BaseFeeToCoinbase {
		paid.Add(paid, new(uint256.Int).Mul(&blk.BaseFee, uint256.NewInt(chainGas)))
	}
	st.AddBalance(blk.Coinbase, paid)
	if a := st.accounts[blk.Coinbase]; a != nil && a.empty() && coinbaseNew {
		delete(st.accounts, blk.Coinbase) // EIP-161: a zero-fee touch leaves no account
	}
	return r, chainGas
}

// ccCheck lists the receipt fields of r that differ from the canonical result.
func ccCheck(r *ExecResult, chainGas uint64, jt *ccTx) []string {
	var bad []string
	if r.Status != jt.Result.Status {
		bad = append(bad, "status")
	}
	if r.Status != "halt" && "0x"+hex.EncodeToString(r.Output) != jt.Result.Output {
		bad = append(bad, "output")
	}
	if ccLogsJSON(r.Logs) != canon(jt.Result.Logs) {
		bad = append(bad, "logs")
	}
	if r.GasGross != jt.Result.GasGross {
		bad = append(bad, "gas_gross")
	}
	if r.GasRefund != min(jt.Result.RefundCounter, jt.Result.GasGross/RefundQuotientEIP3529) {
		bad = append(bad, "refund")
	}
	if chainGas != jt.Result.GasUsed {
		bad = append(bad, "receipt_gas")
	}
	return bad
}

func ccEndOps(st *State, b *ccBlock) {
	for _, op := range b.EndOps {
		a := st.getOrNew(addrOf(op.Address))
		a.balance = u256Of(op.Balance)
		a.nonce = *op.Nonce
	}
}

// SetStartHook sets the hook that sees the State each replay starts from, after the prestate
// build and before the timer (patch sixgg).
func SetStartHook(f func(st *State)) { ccStartHook = f }

// SetBlockHook sets the hook called before each block of the timed loop and once after it
// (patch sixgg).
func SetBlockHook(f func(bi int)) { ccBlockHook = f }

// ccReplay is the timed window replay: one initial State, every transaction
// in order, no checks and no clones inside the timer. Decoding and the
// prestate build happen before it; receipt checks come after it.
// ccTimer brackets the timed region: CC_PERFCTL=ctl,ack gates perf
// (--control fifo) to it, and ccAllocs records the allocations inside it.
var (
	ccTimerHook        func(start bool)
	ccStartHook        func(st *State) // tests: the State each replay starts from
	ccBlockHook        func(bi int)    // called before block bi of the timed loop and once after it (bi = len(blocks)): chunk laps, per-chunk profiles
	ccAllocs, ccAllocB uint64
	ccGCs              uint32
	ccMem0             runtime.MemStats
)

func ccTimer(start bool) {
	if ccTimerHook != nil {
		ccTimerHook(start)
	}
	var m runtime.MemStats
	if start {
		runtime.ReadMemStats(&ccMem0)
		return
	}
	runtime.ReadMemStats(&m)
	ccAllocs, ccAllocB, ccGCs = m.Mallocs-ccMem0.Mallocs, m.TotalAlloc-ccMem0.TotalAlloc, m.NumGC-ccMem0.NumGC
}

func ccReplay(w *ccWindow, asm bool) (time.Duration, int, []string, [][]Receipt, error) {
	st, acc, slots, err := ccPrestate(w)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	if ccStartHook != nil {
		ccStartHook(st)
	}
	curBlock, curTx := 0, 0
	where := func() string {
		b := &w.Blocks[curBlock]
		return fmt.Sprintf("block %d tx %d %s", b.Number, curTx, b.Txs[curTx].Hash)
	}
	blks := make([]BlockContext, len(w.Blocks))
	txs := make([][]Tx, len(w.Blocks))
	txcs := make([][]TxContext, len(w.Blocks))
	n := 0
	for bi := range w.Blocks {
		b := &w.Blocks[bi]
		var failed map[Hash][]byte
		if blks[bi], failed, err = ccBlockCtx(b); err != nil {
			return 0, 0, nil, nil, err
		}
		ccStrict(st, &blks[bi], acc, slots, where)
		for ti := range b.Txs {
			tx, txc := ccTxOf(&b.Txs[ti], failed)
			txs[bi] = append(txs[bi], tx)
			txcs[bi] = append(txcs[bi], txc)
			n++
		}
	}
	res := make([]ExecResult, 0, n)
	gas := make([]uint64, 0, n)
	runtime.GC()
	hugePages()

	ccTimer(true)
	t0 := time.Now()
	for bi := range w.Blocks {
		if ccBlockHook != nil {
			ccBlockHook(bi)
		}
		b := &w.Blocks[bi]
		for _, op := range b.PreOps {
			st.SetStorage(addrOf(op.Address), hashOf(*op.Slot), u256Of(*op.Value))
		}
		coinbaseNew := st.accounts[blks[bi].Coinbase] == nil
		curBlock = bi
		for ti := range txs[bi] {
			curTx = ti
			r, g := ccExec(st, &blks[bi], b.FeeRules, &txs[bi][ti], txcs[bi][ti], b.Txs[ti].FeeFloorGas, coinbaseNew, asm)
			res = append(res, r)
			gas = append(gas, g)
		}
		ccEndOps(st, b)
	}
	if ccBlockHook != nil {
		ccBlockHook(len(w.Blocks))
	}
	el := time.Since(t0)
	ccTimer(false)

	var fails []string
	rcpts := make([][]Receipt, len(w.Blocks))
	i := 0
	for bi := range w.Blocks {
		cum := uint64(0)
		for ti := range w.Blocks[bi].Txs {
			jt := &w.Blocks[bi].Txs[ti]
			cum += gas[i]
			rcpts[bi] = append(rcpts[bi], ccReceipt(jt, &res[i], cum))
			if bad := ccCheck(&res[i], gas[i], jt); len(bad) > 0 {
				fails = append(fails, fmt.Sprintf("block %d tx %d %s: %v", w.Blocks[bi].Number, ti, jt.Hash, bad))
			}
			i++
		}
	}
	return el, n, fails, rcpts, nil
}

// ccWindowFiles lists the promotable windows (failed-* files do not match).
func ccWindowFiles() []string {
	files, _ := filepath.Glob(filepath.Join(cchainFixturesDir, "window-*.json.gz"))
	return files
}

// ccSettlementFile holds every window header, each window's predecessor
// header and the tail headers that settle the last window blocks. C-Chain
// runs SAE (ACP-194): header n's receiptsRoot is DeriveSha over the
// concatenated receipts of the blocks it newly settles, (settledHeight(n-1),
// settledHeight(n)] (avalanchego vms/saevm/sae/block_builder.go), each
// receipt keeping its own block's cumulative gas.
const ccSettlementFile = "../../faster-evm/comparison-20261018/cchain-receipt-headers/settlement-headers.json.gz"

type ccHeader struct {
	Hash          string `json:"hash"`
	Number        string `json:"number"`
	SettledHeight string `json:"settledHeight"`
	ReceiptsRoot  string `json:"receiptsRoot"`
	ExtraData     string `json:"extraData"`
}

type ccSettlement struct {
	Format   string `json:"format"`
	Complete bool   `json:"complete"`
	Windows  []struct {
		FixtureSHA256 string `json:"fixture_sha256"`
		Fixture       string `json:"fixture"`
	} `json:"windows"`
	Headers map[string]*ccHeader `json:"headers"`
	byNum   map[uint64]*ccHeader
}

var ccSettle *ccSettlement

// ccSettlementLoad reads the sidecar once.
func ccSettlementLoad() (*ccSettlement, error) {
	if ccSettle != nil {
		return ccSettle, nil
	}
	f, err := os.Open(ccSettlementFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var s ccSettlement
	if err := json.NewDecoder(gz).Decode(&s); err != nil {
		return nil, err
	}
	if s.Format != "cchain-settlement-evidence/v1" || !s.Complete {
		return nil, fmt.Errorf("settlement sidecar format %q complete %v", s.Format, s.Complete)
	}
	s.byNum = map[uint64]*ccHeader{}
	for h, x := range s.Headers {
		if !strings.EqualFold(h, x.Hash) {
			return nil, fmt.Errorf("header key %s holds hash %s", h, x.Hash)
		}
		s.byNum[ccHexU64(x.Number)] = x
	}
	ccSettle = &s
	return ccSettle, nil
}

func ccHexU64(h string) uint64 {
	v, err := strconv.ParseUint(strings.TrimPrefix(h, "0x"), 16, 64)
	if err != nil {
		panic(err)
	}
	return v
}

// ccBind checks that the sidecar binds this exact fixture file.
func ccBind(s *ccSettlement, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	for _, sw := range s.Windows {
		if sw.Fixture == filepath.Base(path) {
			if sw.FixtureSHA256 != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("%s: sidecar binds sha256 %s, file is %x", sw.Fixture, sw.FixtureSHA256, sum)
			}
			return nil
		}
	}
	return fmt.Errorf("%s: no settlement entry", filepath.Base(path))
}

// ccRoots is the after-the-timer receipts-root outcome of one window.
type ccRoots struct {
	candidates, nonempty, empty, skipped, covered int
	final                                         uint64
	unchecked                                     []uint64 // window blocks no checked root covers
	bad                                           []string
}

// ccRootCheck walks every header from the first window block through the
// header that settles the last one. An empty settled range must carry the
// empty root; a range inside the window is checked against the DeriveSha
// of our replay's receipts; a range reaching before or past the window is
// skipped.
func ccRootCheck(path string, w *ccWindow, rcpts [][]Receipt) (*ccRoots, error) {
	s, err := ccSettlementLoad()
	if err != nil {
		return nil, err
	}
	if err := ccBind(s, path); err != nil {
		return nil, err
	}
	out := &ccRoots{}
	covered := make([]bool, len(w.Blocks))
	for n := w.First; ; n++ {
		h, p := s.byNum[n], s.byNum[n-1]
		if h == nil || p == nil {
			return nil, fmt.Errorf("header %d or its parent missing: the last window block is not settled in the sidecar", n)
		}
		if b := &w.Blocks[min(n-w.First, uint64(len(w.Blocks)-1))]; n <= w.Last && !strings.EqualFold(b.Hash, h.Hash) {
			return nil, fmt.Errorf("block %d: fixture hash %s, header %s", n, b.Hash, h.Hash)
		}
		lo, hi := ccHexU64(p.SettledHeight)+1, ccHexU64(h.SettledHeight)
		out.candidates++
		var rs []Receipt
		check := true
		switch {
		case lo > hi:
			out.empty++
		case lo < w.First || hi > w.Last:
			out.skipped++
			check = false
		default:
			out.nonempty++
			for b := lo; b <= hi; b++ {
				rs = append(rs, rcpts[b-w.First]...)
				covered[b-w.First] = true
			}
		}
		if got := "0x" + ccHexHash(ReceiptsRoot(rs)); check && got != h.ReceiptsRoot {
			out.bad = append(out.bad, fmt.Sprintf("header %d (blocks %d-%d): receipts root %s, header %s", n, lo, hi, got, h.ReceiptsRoot))
		}
		if hi >= w.Last {
			out.final = n
			break
		}
	}
	for i, c := range covered {
		if c {
			out.covered++
		} else {
			out.unchecked = append(out.unchecked, w.First+uint64(i))
		}
	}
	return out, nil
}

func (r *ccRoots) String() string {
	return fmt.Sprintf("%d candidate headers through %d = %d nonempty checked + %d empty checked + %d skipped; %d bad; %d window blocks root-covered, unchecked by root %v",
		r.candidates, r.final, r.nonempty, r.empty, r.skipped, len(r.bad), r.covered, r.unchecked)
}

// ccReceipt is the consensus receipt of one replayed transaction.
func ccReceipt(jt *ccTx, r *ExecResult, cum uint64) Receipt {
	return Receipt{Type: jt.Type, Success: r.Status == "success", CumGas: cum, Logs: r.Logs}
}

func canon(v any) string { b, _ := json.Marshal(v); return string(b) }

// WindowRun is one timed replay of a window and its after-the-timer checks.
type WindowRun struct {
	Name                     string
	Blocks, Txs, Unsupported int
	Gas                      uint64 // canonical receipt gas of all txs
	Elapsed                  time.Duration
	Fails                    []string // txs whose receipt fields differ
	Roots                    *ccRoots
	Allocs, AllocBytes       uint64 // inside the timer
	GCs                      uint32
}

// Passed is the gate: every tx matches, every checked root matches, nothing unsupported.
func (r *WindowRun) Passed() bool {
	return len(r.Fails) == 0 && len(r.Roots.bad) == 0 && r.Unsupported == 0
}

func (r *WindowRun) String() string {
	s := r.Elapsed.Seconds()
	return fmt.Sprintf("blocks=%d txs=%d unsupported=%d wall_ms=%.3f us_per_tx=%.2f mgas_per_s=%.0f txs_compared=%d mismatched_txs=%d roots: %v",
		r.Blocks, r.Txs, r.Unsupported, s*1000, s*1e6/float64(r.Txs), float64(r.Gas)/s/1e6, r.Txs, len(r.Fails), r.Roots)
}

// ReplayWindow loads the window at path (decode and prestate build untimed),
// times the ordered replay of all its txs from one initial state, then checks
// every receipt and the settlement receipts roots. asm selects the native core.
func ReplayWindow(path string, asm bool) (*WindowRun, error) {
	w, why, err := ccLoad(path)
	if err != nil {
		return nil, err
	}
	el, n, fails, rcpts, err := ccReplay(w, asm)
	if err != nil {
		return nil, err
	}
	roots, err := ccRootCheck(path, w, rcpts)
	if err != nil {
		return nil, err
	}
	r := &WindowRun{Name: filepath.Base(path), Blocks: len(w.Blocks), Txs: n, Unsupported: len(why), Elapsed: el, Fails: fails, Roots: roots,
		Allocs: ccAllocs, AllocBytes: ccAllocB, GCs: ccGCs}
	for _, b := range w.Blocks {
		for _, t := range b.Txs {
			r.Gas += t.Result.GasUsed
		}
	}
	return r, nil
}
