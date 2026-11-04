package settleledger

import (
	"encoding/binary"
	"math/big"
	"os"
	"strings"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/holiman/uint256"
)

// A settled row, entirely inside one address's storage.
//
// The Solidity version spends its row across three contracts: the settlement contract
// holds party entitlement, two ERC-20s hold custody, and a registry answers who a party
// clears through. Eleven external calls per row follow from that split, and each one is
// 700 gas before its arguments are encoded and decoded.
//
// Here all four ledgers live at this precompile's address, so a row is arithmetic on
// slots with no calls at all.
//
// Gas is charged at one per row, which is nominal on purpose. A precompile is billed by
// whatever its author writes, so any figure here is an assertion rather than a
// measurement, and gas is a budget rather than a speed: it decides how many
// transactions fit under the block gas limit and has no bearing on how long the work
// takes. With the row executing here, blocks run under 3% of the gas limit and about
// 900 KB of calldata, so a block is bounded by bytes. Pricing a row at one puts the gas
// budget out of the picture by construction instead of leaving it arguable.
//
// What the port costs is stated on canHold below, where it is incurred.
const (
	perCallGas = 1
	maxRows    = 400 // one transport envelope
	rowWords   = 7   // ref, security, cash, seller, buyer, quantity, consideration
)

var (
	selSettle  = crypto.Keccak256([]byte("settleRows(bytes)"))[:4]
	selPacked  = crypto.Keccak256([]byte("settlePacked(bytes)"))[:4]
	selSetTok  = crypto.Keccak256([]byte("setTokenAt(uint256,address)"))[:4]
	selSetPty  = crypto.Keccak256([]byte("setPartyAt(uint256,address)"))[:4]
	selSetCash = crypto.Keccak256([]byte("setCashToken(address)"))[:4]
	selShadow  = crypto.Keccak256([]byte("shadowBalanceOf(address,address)"))[:4]
	selHeld    = crypto.Keccak256([]byte("heldBalanceOf(address,address)"))[:4]
	selCredit  = crypto.Keccak256([]byte("credit(address,address,address,uint256)"))[:4]
	// creditPacked seeds many positions in one call. Opening a position per party per
	// instrument is one transaction each through credit, which is 2,000 x 3,000 = six
	// million transactions for a venue-sized world — days of setup before a measurement
	// can start. Packed, the same work is four bytes a position.
	selCreditPacked = crypto.Keccak256([]byte("creditPacked(uint256,bytes)"))[:4]
	selBind         = crypto.Keccak256([]byte("bind(address,address)"))[:4]
	selAllowed      = crypto.Keccak256([]byte("setHolderAllowed(address,bool)"))[:4]

	// The observer reconciles accepted work by this exact preimage, so the event this
	// precompile emits has to be byte-identical to the Solidity one.
	settledTopic = crypto.Keccak256([]byte(
		"Settled(uint256,bytes32,address,address,address,address,address,uint256,uint256)"))

	settledTopicHash = common.BytesToHash(settledTopic)

	// SettleBatch has no Solidity counterpart, so nothing outside this file decodes it
	// against a fixed ABI signature the way the observer does for Settled. The string
	// below exists only to produce a distinct, self-documenting topic hash; the actual
	// layout of the data is the one emitSettleBatch writes.
	settleBatchTopic = crypto.Keccak256([]byte(
		"SettleBatch(uint256,uint256,bytes,bytes,bytes,bytes,bytes,bytes,bytes,bytes)"))

	settleBatchTopicHash = common.BytesToHash(settleBatchTopic)
)

var SettleLedgerPrecompile = func() contract.StatefulPrecompiledContract {
	c, err := contract.NewStatefulPrecompileContract(nil, []*contract.StatefulPrecompileFunction{
		contract.NewStatefulPrecompileFunction(selSettle, runSettleRows),
		contract.NewStatefulPrecompileFunction(selPacked, runSettlePacked),
		contract.NewStatefulPrecompileFunction(selSetTok, setter(tokenAtKey)),
		contract.NewStatefulPrecompileFunction(selSetPty, setter(partyAtKey)),
		contract.NewStatefulPrecompileFunction(selSetCash, runSetCashToken),
		contract.NewStatefulPrecompileFunction(selShadow, reader(shadowKey)),
		contract.NewStatefulPrecompileFunction(selHeld, reader(heldKey)),
		contract.NewStatefulPrecompileFunction(selCredit, runCredit),
		contract.NewStatefulPrecompileFunction(selCreditPacked, runCreditPacked),
		contract.NewStatefulPrecompileFunction(selBind, runBind),
		contract.NewStatefulPrecompileFunction(selAllowed, runSetHolderAllowed),

		// The ERC-20 surface, in erc20.go. It is a second way in to the same balances,
		// not a second copy of them, and it is deliberately not on the batch path.
		contract.NewStatefulPrecompileFunction(selErc20BalanceOf, runErc20BalanceOf),
		contract.NewStatefulPrecompileFunction(selErc20Entitlement, runErc20Entitlement),
		contract.NewStatefulPrecompileFunction(selErc20TotalSupply, runErc20TotalSupply),
		contract.NewStatefulPrecompileFunction(selErc20Allowance, runErc20Allowance),
		contract.NewStatefulPrecompileFunction(selErc20CanHold, runErc20CanHold),
		contract.NewStatefulPrecompileFunction(selErc20Approve, runErc20Approve),
		contract.NewStatefulPrecompileFunction(selErc20Transfer, runErc20Transfer),
		contract.NewStatefulPrecompileFunction(selErc20TransferFrom, runErc20TransferFrom),
		contract.NewStatefulPrecompileFunction(selErc20Moves, runErc20Moves),
	})
	if err != nil {
		panic(err)
	}
	return c
}()

// Each ledger gets its own keyspace. The trailing byte is a domain separator: without
// it a (token, party) pair and a (token, holder) pair could hash to the same slot and
// entitlement would silently alias custody.
const (
	shadowDomain  byte = 1
	heldDomain    byte = 2
	bindingDomain byte = 3
	allowedDomain byte = 4
	seqDomain     byte = 5
	// Domain 6 is the handled bitmap; its keys are derived from references in handledKey.
	tokenIndexDomain byte = 7
	partyIndexDomain byte = 8
	cashDomain       byte = 9
)

// slotWith derives a ledger slot on a caller-owned hasher, so a hot path can reuse one
// hasher across derivations instead of allocating a fresh state per key.
func slotWith(kh crypto.KeccakState, domain byte, a, b common.Address) common.Hash {
	var buf [65]byte
	copy(buf[12:32], a[:])
	copy(buf[44:64], b[:])
	buf[64] = domain
	return crypto.HashData(kh, buf[:])
}

func slot(domain byte, a, b common.Address) common.Hash {
	return slotWith(crypto.NewKeccakState(), domain, a, b)
}

func shadowKey(token, party common.Address) common.Hash { return slot(shadowDomain, token, party) }
func heldKey(token, holder common.Address) common.Hash  { return slot(heldDomain, token, holder) }
func bindingKey(party common.Address) common.Hash {
	return slot(bindingDomain, common.Address{}, party)
}
func allowedKey(holder common.Address) common.Hash {
	return slot(allowedDomain, common.Address{}, holder)
}

// The sequence and cash-token slots take no argument, so their keys are constants of
// the keyspace and are derived once.
var (
	seqSlot  = slot(seqDomain, common.Address{}, common.Address{})
	cashSlot = slot(cashDomain, common.Address{}, common.Address{})
)

func seqKey() common.Hash  { return seqSlot }
func cashKey() common.Hash { return cashSlot }

// Index tables let a row name a token or a party in one or two bytes instead of
// thirty-two. Once a row stops costing gas, calldata is what bounds a block, so the
// encoding is the throughput lever rather than a tidiness one.
func indexKey(domain byte, i uint64) common.Hash {
	return indexKeyWith(crypto.NewKeccakState(), domain, i)
}

func indexKeyWith(kh crypto.KeccakState, domain byte, i uint64) common.Hash {
	var buf [33]byte
	binary.BigEndian.PutUint64(buf[24:32], i)
	buf[32] = domain
	return crypto.HashData(kh, buf[:])
}
func tokenAtKey(i uint64) common.Hash { return indexKey(tokenIndexDomain, i) }
func partyAtKey(i uint64) common.Hash { return indexKey(partyIndexDomain, i) }

// packedRowBytes describes one execution in 26 bytes:
//
//	 0..7   journal reference   uint64
//	 8..9   security            token index, uint16
//	10..11  seller              party index, uint16
//	12..13  buyer               party index, uint16
//	14..17  quantity            uint32
//	18..25  consideration       uint64
//
// The security index is two bytes because one caps a venue at 256 instruments, and a
// listed market carries thousands. Widening it changes how every row is read, so a chain
// cannot adopt it: re-executing an existing block would parse its rows differently and
// produce a different state root. It belongs to a chain from genesis.
//
// The cash token is configured once rather than carried per row, because every row in
// this workload settles against the same one.
const packedRowBytes = 26

// handledKey puts consecutive references in one word, so a run of them costs a single
// read and a single write instead of a fresh slot each. The batch resolves it through
// handledCell, which derives each block's key once for the run that shares it.
func handledKey(kh crypto.KeccakState, ref *uint256.Int) (common.Hash, uint) {
	var buf [33]byte
	var block uint256.Int
	block.Rsh(ref, 8)
	w := block.Bytes32()
	copy(buf[0:32], w[:])
	buf[32] = 6
	return crypto.HashData(kh, buf[:]), uint(ref[0] & 255)
}

type row struct {
	ref                     uint256.Int
	security, cash          common.Address
	seller, buyer           common.Address
	quantity, consideration uint256.Int
}

// decode reads a flat byte string rather than an ABI array of structs. The layout is
// seven words per row with no per-row offsets, because ABI array decoding is part of
// the overhead this precompile exists to remove.
func decode(input []byte) ([]row, bool) {
	if len(input) < 64 {
		return nil, false
	}
	size := new(big.Int).SetBytes(input[32:64]).Uint64()
	body := input[64:]
	if uint64(len(body)) < size || size == 0 || size%(rowWords*32) != 0 {
		return nil, false
	}
	n := size / (rowWords * 32)
	if n > maxRows {
		return nil, false
	}
	out := make([]row, n)
	for i := uint64(0); i < n; i++ {
		w := body[i*rowWords*32:]
		r := &out[i]
		r.ref.SetBytes(w[0:32])
		r.security = common.BytesToAddress(w[44:64])
		r.cash = common.BytesToAddress(w[76:96])
		r.seller = common.BytesToAddress(w[108:128])
		r.buyer = common.BytesToAddress(w[140:160])
		r.quantity.SetBytes(w[160:192])
		r.consideration.SetBytes(w[192:224])
	}
	return out, true
}

type ledger struct {
	state contract.StateDB
	addr  common.Address
}

func (l ledger) get(k common.Hash) *big.Int {
	return new(big.Int).SetBytes(l.state.GetState(l.addr, k).Bytes())
}
func (l ledger) set(k common.Hash, v *big.Int) {
	l.state.SetState(l.addr, k, common.BigToHash(v))
}

// holderOf resolves the omnibus a party clears through. A party bound to nothing cannot
// settle: resolving to the zero address would let an unbound party trade against slot
// zero, which is why this returns a flag rather than an address alone.
func (l ledger) holderOf(party common.Address) (common.Address, bool) {
	h := common.BigToAddress(l.get(bindingKey(party)))
	return h, h != (common.Address{})
}

// canHold is the check RestrictedToken enforces structurally, and this is the cost of
// the port: in Solidity every transfer path runs through _move, so a party that may not
// hold cannot receive by any route, whoever calls. Here it is a branch in one function.
//
// The guarantee holds only while every write to custody goes through moveHeld. That is
// true of all three writers — the batch path, the ERC-20 surface and credit below — and
// it is what a fourth would have to do. Nothing enforces it; a write straight to a
// heldKey slot would bypass the rule and nothing would complain.
func (l ledger) canHold(holder common.Address) bool {
	return l.get(allowedKey(holder)).Sign() != 0
}

func (l ledger) moveShadow(token, from, to common.Address, amount *big.Int) bool {
	fk, tk := shadowKey(token, from), shadowKey(token, to)
	bal := l.get(fk)
	if bal.Cmp(amount) < 0 {
		return false
	}
	l.set(fk, new(big.Int).Sub(bal, amount))
	l.set(tk, new(big.Int).Add(l.get(tk), amount))
	return true
}

func (l ledger) moveHeld(token, from, to common.Address, amount *big.Int) bool {
	if !l.canHold(to) {
		return false
	}
	fk, tk := heldKey(token, from), heldKey(token, to)
	bal := l.get(fk)
	if bal.Cmp(amount) < 0 {
		return false
	}
	l.set(fk, new(big.Int).Sub(bal, amount))
	l.set(tk, new(big.Int).Add(l.get(tk), amount))
	return true
}

// runSettleRows applies a batch. A row that cannot settle is skipped rather than
// reverting the batch, because one short party must not cost the other 199 rows their
// settlement; the returned bitmap tells the caller which rows applied.
//
// Both legs of a row move together or neither does. The security leg is undone if the
// cash leg fails, which is the same atomicity the Solidity version gets from sharing a
// call frame with its two token calls.
// runSettlePacked settles rows described in 25 bytes each rather than 224. The work is
// identical; only how the caller names tokens and parties differs, and that is what
// decides how many rows fit in a block once gas has stopped being the constraint.
func runSettlePacked(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	if len(input) < 64 {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	size := new(big.Int).SetBytes(input[32:64]).Uint64()
	body := input[64:]
	if uint64(len(body)) < size || size == 0 || size%packedRowBytes != 0 {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	n := size / packedRowBytes
	if n > maxRows {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas*n)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remaining, vm.ErrWriteProtection
	}
	b := newBatch(evm.GetStateDB(), addr)
	cash := common.Address(b.get(cashKey()).Bytes20())
	rows := make([]row, n)
	for i := uint64(0); i < n; i++ {
		r := body[i*packedRowBytes:]
		o := &rows[i]
		o.ref.SetUint64(be(r[0:8]))
		o.security = b.addressAt(tokenIndexDomain, be(r[8:10]))
		o.cash = cash
		o.seller = b.addressAt(partyIndexDomain, be(r[10:12]))
		o.buyer = b.addressAt(partyIndexDomain, be(r[12:14]))
		o.quantity.SetUint64(be(r[14:18]))
		o.consideration.SetUint64(be(r[18:26]))
	}
	return apply(b, evm, addr, rows, remaining)
}

// be reads a big-endian unsigned integer of up to eight bytes.
func be(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

func setter(key func(uint64) common.Hash) contract.RunStatefulPrecompileFunc {
	return func(
		evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
	) ([]byte, uint64, error) {
		remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas)
		if err != nil {
			return nil, 0, err
		}
		if readOnly {
			return nil, remaining, vm.ErrWriteProtection
		}
		if len(input) < 64 {
			return nil, remaining, vm.ErrExecutionReverted
		}
		i := new(big.Int).SetBytes(input[0:32]).Uint64()
		v := new(big.Int).SetBytes(input[44:64])
		ledger{state: evm.GetStateDB(), addr: addr}.set(key(i), v)
		return nil, remaining, nil
	}
}

func runSetCashToken(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remaining, vm.ErrWriteProtection
	}
	if len(input) < 32 {
		return nil, remaining, vm.ErrExecutionReverted
	}
	ledger{state: evm.GetStateDB(), addr: addr}.set(cashKey(), new(big.Int).SetBytes(input[12:32]))
	return nil, remaining, nil
}

func runSettleRows(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	rows, ok := decode(input)
	if !ok {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas*uint64(len(rows)))
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remaining, vm.ErrWriteProtection
	}
	return apply(newBatch(evm.GetStateDB(), addr), evm, addr, rows, remaining)
}

// The eight outcomes a row can have, in the order their bit appears in the batch log
// emitted by emitSettleBatch. They are mutually exclusive: apply gives each row exactly
// one, so a consumer testing whether anything failed ORs every bitmap but settled.
const (
	reasonSettled = iota
	reasonDuplicate
	reasonMalformed
	reasonUnbound
	reasonShortSecurity
	reasonShortCash
	reasonShortCustodySecurity
	reasonShortCustodyCash
	reasonCount
)

func setBit(bitmap []byte, i int) { bitmap[i>>3] |= 1 << uint(i&7) }

// apply settles a batch. A row that cannot settle is skipped rather than reverting the
// batch, because one short party must not cost the other rows their settlement; the
// returned bitmap tells the caller which rows applied.
//
// Both legs of a row move together or neither does. The security leg is undone if the
// cash leg fails, which is the same atomicity the Solidity version gets from sharing a
// call frame with its two token calls.
func apply(
	b *batch, evm contract.AccessibleState, addr common.Address, rows []row, remaining uint64,
) ([]byte, uint64, error) {
	seqCell := b.cell(seqKey())
	var startSeq uint256.Int
	startSeq.Set(&seqCell.value)
	blockNum := evm.GetBlockContext().Number().Uint64()
	one := uint256.NewInt(1)

	bitmapBytes := (len(rows) + 7) / 8
	var bitmaps [reasonCount][]byte
	for i := range bitmaps {
		bitmaps[i] = make([]byte, bitmapBytes)
	}
	applied := bitmaps[reasonSettled]

	for i := range rows {
		r := &rows[i]
		if r.quantity.IsZero() || r.consideration.IsZero() || r.seller == r.buyer {
			setBit(bitmaps[reasonMalformed], i)
			continue
		}
		// The replay bitmap is a slot like any other, so the cache holds the word for as
		// long as the batch needs it rather than this loop tracking one open word.
		handled, bit := b.handledCell(&r.ref)
		wordIdx, mask := bit>>6, uint64(1)<<(bit&63)
		if handled.value[wordIdx]&mask != 0 {
			setBit(bitmaps[reasonDuplicate], i) // a terminal outcome already exists for this reference
			continue
		}
		sellerHolder, sOK := b.holderOf(r.seller)
		buyerHolder, bOK := b.holderOf(r.buyer)
		if !sOK || !bOK {
			setBit(bitmaps[reasonUnbound], i)
			continue
		}
		if !b.moveShadow(r.security, r.seller, r.buyer, &r.quantity) {
			setBit(bitmaps[reasonShortSecurity], i)
			continue
		}
		if !b.moveShadow(r.cash, r.buyer, r.seller, &r.consideration) {
			b.moveShadow(r.security, r.buyer, r.seller, &r.quantity)
			setBit(bitmaps[reasonShortCash], i)
			continue
		}
		// Custody only moves when the parties clear through different omnibuses. Within
		// one omnibus the assets never left it; only the attribution changed.
		if sellerHolder != buyerHolder {
			if !b.moveHeld(r.security, sellerHolder, buyerHolder, &r.quantity) {
				b.moveShadow(r.security, r.buyer, r.seller, &r.quantity)
				b.moveShadow(r.cash, r.seller, r.buyer, &r.consideration)
				setBit(bitmaps[reasonShortCustodySecurity], i)
				continue
			}
			if !b.moveHeld(r.cash, buyerHolder, sellerHolder, &r.consideration) {
				b.moveHeld(r.security, buyerHolder, sellerHolder, &r.quantity)
				b.moveShadow(r.security, r.buyer, r.seller, &r.quantity)
				b.moveShadow(r.cash, r.seller, r.buyer, &r.consideration)
				setBit(bitmaps[reasonShortCustodyCash], i)
				continue
			}
		}
		seqCell.value.Add(&seqCell.value, one)
		if !logsDisabled && perRowSettledEvents {
			emitSettled(b.state, addr, blockNum, &seqCell.value, r, sellerHolder, buyerHolder)
		}
		handled.value[wordIdx] |= mask
		setBit(applied, i)
	}
	if !logsDisabled {
		emitSettleBatch(b.state, addr, blockNum, &startSeq, uint64(len(rows)), bitmaps)
	}
	b.flush()

	out := make([]byte, 64+((len(applied)+31)/32)*32)
	out[31] = 32
	length := uint256.NewInt(uint64(len(applied))).Bytes32()
	copy(out[56:64], length[24:32])
	copy(out[64:], applied)
	return out, remaining, nil
}

// logsDisabled drops every event this engine emits: the batch log every call writes,
// and the per-row Settled log perRowSettledEvents adds. A log is carried into the
// block's receipts and its bloom filter regardless of how many rows it names, so a run
// with logsDisabled set separates the cost of settling a row from the cost of
// reporting it. A consumer reading settlement from events sees nothing with this set,
// so it is a measurement setting and not an operating one.
// The VM runs as a process avalanchego spawns with an environment of its own choosing, so a
// variable set on the container reaches the node and not the engine. Settings are read from
// files in the mounted chain-config directory instead, which the VM process can see.
func settingFile(name string) string {
	for _, dir := range []string{"/chainconf", "/opt/settlement/chainconf"} {
		if b, err := os.ReadFile(dir + "/" + name); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

var logsDisabled = settingFile("settle-no-logs") == "1"

// perRowSettledEvents additionally emits the per-row Settled event for every settled
// row, alongside the batch log. bkexpress/internal/chain/observer.go reads settlement
// through FilterLogs against Settled; this keeps that path working until the observer
// reads the batch log instead. It has no effect when logsDisabled is set, since that
// silences every event regardless.
var perRowSettledEvents = settingFile("settle-per-row-events") == "1"

// perRowGas is what a row is charged beyond its calldata. At 1 it is nominal, and a block
// then holds whatever the pool is offering rather than a bounded number of rows: gas stops
// acting as the governor it is meant to be, and a validator spends as long executing a
// block as the backlog happens to be deep.
//
// Setting it to the block gas limit divided by the rows a block should carry makes
// rows-per-block a property of the chain instead of a property of how hard clients are
// pushing. It dominates the 400 gas of calldata a row already costs, so the cap it sets is
// close to exact rather than shifting with batch size.
//
// Every validator must agree on it: gas used goes into the block header, so a node with a
// different value computes a different header and cannot agree with the rest.
var perRowGas = func() uint64 {
	if v := settingFile("settle-row-gas"); v != "" {
		var n uint64
		for _, c := range v {
			if c < '0' || c > '9' {
				return 1
			}
			n = n*10 + uint64(c-'0')
		}
		if n > 0 {
			return n
		}
	}
	return 1
}()

// emitSettled writes the same event the Solidity contract does, with the same topics
// and the same seven data words, so the observer and the receipt audit need no change
// while perRowSettledEvents is set.
//
// It names the security, both parties, both holders, the quantity and the
// consideration for one settled row — detail the batch log does not carry, in exchange
// for one log per settled row rather than one per batch. No ERC-20 Transfer is written
// for the two custody legs, and that is the one place the ERC-20 surface is knowingly incomplete:
// an indexer that reconstructs balances from Transfer logs alone will drift from what
// balanceOf answers, while everything that reads state — a wallet, a bridge, another
// contract — stays exact. Three reasons for taking it that way rather than emitting
// them:
//
//   - A 100-row batch already carries about a hundred logs and ten batches of receipts
//     was enough to exceed the node's JSON-RPC response size during the audit; two more
//     logs per row triples that and breaks the auditor that exists.
//   - Settled already names the security, both parties, both holders, the quantity and
//     the consideration. A pair of Transfers is a strictly poorer record of the same
//     movement, and it cannot express that the two legs were one row.
//   - The Solidity design declined the same trade for the same reason, in
//     SponsoredSettle._moveShadow: "Two LOG4s per row is a high price for restating it."
//
// A consumer that needs a per-leg log stream should derive it from Settled, which has
// the information; it cannot be recovered from Transfer, which does not.
func emitSettled(
	state contract.StateDB, addr common.Address, blockNum uint64,
	seq *uint256.Int, r *row, sellerHolder, buyerHolder common.Address,
) {
	data := make([]byte, 7*32)
	copy(data[12:32], r.security[:])
	copy(data[44:64], r.seller[:])
	copy(data[76:96], r.buyer[:])
	copy(data[108:128], sellerHolder[:])
	copy(data[140:160], buyerHolder[:])
	q := r.quantity.Bytes32()
	copy(data[160:192], q[:])
	c := r.consideration.Bytes32()
	copy(data[192:224], c[:])
	state.AddLog(&types.Log{
		Address: addr,
		Topics: []common.Hash{
			settledTopicHash,
			common.Hash(seq.Bytes32()),
			common.Hash(r.ref.Bytes32()),
		},
		Data:        data,
		BlockNumber: blockNum,
	})
}

// emitSettleBatch writes one log for the whole batch: eight bitmaps, one bit per row in
// the order rows were given, naming which of the eight outcomes in the reason constants
// each row had. They are mutually exclusive, so a consumer testing whether anything
// failed ORs the seven bitmaps after settled, and a reason added later takes the next
// index without moving the bits already assigned to the others.
//
// The topic carries the batch's starting sequence number: the value seqKey held before
// this batch's rows were applied. A settled row's own sequence number is startSeq plus
// its rank among the set bits of the settled bitmap up to and including its own
// position, which is enough for a consumer to recover per-row sequence numbers from the
// bitmap alone.
//
// The data is row count, then the eight bitmaps back to back, each padded out to a
// whole number of words so a fixed offset locates every one of them from rowCount alone.
func emitSettleBatch(
	state contract.StateDB, addr common.Address, blockNum uint64,
	startSeq *uint256.Int, rowCount uint64, bitmaps [reasonCount][]byte,
) {
	block := ((len(bitmaps[reasonSettled]) + 31) / 32) * 32
	data := make([]byte, 32+reasonCount*block)
	rc := uint256.NewInt(rowCount).Bytes32()
	copy(data[0:32], rc[:])
	for i, bm := range bitmaps {
		copy(data[32+i*block:], bm)
	}
	state.AddLog(&types.Log{
		Address: addr,
		Topics: []common.Hash{
			settleBatchTopicHash,
			common.Hash(startSeq.Bytes32()),
		},
		Data:        data,
		BlockNumber: blockNum,
	})
}

func reader(key func(a, b common.Address) common.Hash) contract.RunStatefulPrecompileFunc {
	return func(
		evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, _ bool,
	) ([]byte, uint64, error) {
		remaining, err := contract.DeductGas(suppliedGas, perCallGas)
		if err != nil {
			return nil, 0, err
		}
		if len(input) < 64 {
			return nil, remaining, vm.ErrExecutionReverted
		}
		a := common.BytesToAddress(input[12:32])
		b := common.BytesToAddress(input[44:64])
		return evm.GetStateDB().GetState(addr, key(a, b)).Bytes(), remaining, nil
	}
}

// runCredit seeds entitlement and custody together, so a seeded world starts backed:
// the same amount lands in the party's shadow balance and in its holder's custody.
// Its backing outside this chain is an assumption, as it is for the Solidity admin path.
//
// It is issuance, so it moves total supply and it applies the holding restriction, both
// for the reason RestrictedToken.mint does: supply is what an ERC-20 holder is told when
// it asks, and a credit to an address that may not hold would create a balance no
// transfer could have created.
func runCredit(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remaining, vm.ErrWriteProtection
	}
	if len(input) < 128 {
		return nil, remaining, vm.ErrExecutionReverted
	}
	l := ledger{state: evm.GetStateDB(), addr: addr}
	token := common.BytesToAddress(input[12:32])
	party := common.BytesToAddress(input[44:64])
	holder := common.BytesToAddress(input[76:96])
	amount := new(big.Int).SetBytes(input[96:128])
	if !l.canHold(holder) {
		return nil, remaining, vm.ErrExecutionReverted
	}
	sk, hk, pk := shadowKey(token, party), heldKey(token, holder), supplyKey(token)
	l.set(sk, new(big.Int).Add(l.get(sk), amount))
	l.set(hk, new(big.Int).Add(l.get(hk), amount))
	l.set(pk, new(big.Int).Add(l.get(pk), amount))
	return nil, remaining, nil
}

// packedPositionBytes is one position to open: an instrument and a party, both by the
// index the registries already hold.
const packedPositionBytes = 4

// runCreditPacked opens many positions at one amount. The holder comes from the party's
// binding rather than the caller, which is where settlement reads it from too: a seeded
// world is then bound the same way a settled one is, instead of by whatever the setup
// passed alongside.
// add moves a cell by an amount, which is what opening a position and settling a row both
// do to a balance.
func add(c *cell, amount *uint256.Int) { c.value.Add(&c.value, amount) }

func runCreditPacked(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	if readOnly {
		return nil, suppliedGas, vm.ErrWriteProtection
	}
	// amount, then the offset and length of the packed body, in the ABI's own layout.
	if len(input) < 96 {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	var amt uint256.Int
	amt.SetBytes(input[0:32])
	size := new(big.Int).SetBytes(input[64:96]).Uint64()
	body := input[96:]
	if uint64(len(body)) < size || size == 0 || size%packedPositionBytes != 0 {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	n := size / packedPositionBytes
	if n > maxRows {
		return nil, suppliedGas, vm.ErrExecutionReverted
	}
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas*n)
	if err != nil {
		return nil, 0, err
	}

	b := newBatch(evm.GetStateDB(), addr)
	for i := uint64(0); i < n; i++ {
		r := body[i*packedPositionBytes:]
		token := b.addressAt(tokenIndexDomain, be(r[0:2]))
		party := b.addressAt(partyIndexDomain, be(r[2:4]))
		if token == (common.Address{}) || party == (common.Address{}) {
			return nil, remaining, vm.ErrExecutionReverted
		}
		holder, bound := b.holderOf(party)
		if !bound || !b.canHold(holder) {
			return nil, remaining, vm.ErrExecutionReverted
		}
		add(b.cellFor(shadowDomain, token, party), &amt)
		add(b.cellFor(heldDomain, token, holder), &amt)
		add(b.cellFor(10, token, common.Address{}), &amt)
	}
	b.flush()
	return nil, remaining, nil
}

func runBind(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remaining, vm.ErrWriteProtection
	}
	if len(input) < 64 {
		return nil, remaining, vm.ErrExecutionReverted
	}
	l := ledger{state: evm.GetStateDB(), addr: addr}
	party := common.BytesToAddress(input[12:32])
	holder := common.BytesToAddress(input[44:64])
	l.set(bindingKey(party), new(big.Int).SetBytes(holder[:]))
	return nil, remaining, nil
}

func runSetHolderAllowed(
	evm contract.AccessibleState, _, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
) ([]byte, uint64, error) {
	remaining, err := contract.DeductGas(suppliedGas, perCallGas+perRowGas)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remaining, vm.ErrWriteProtection
	}
	if len(input) < 64 {
		return nil, remaining, vm.ErrExecutionReverted
	}
	l := ledger{state: evm.GetStateDB(), addr: addr}
	holder := common.BytesToAddress(input[12:32])
	allowed := new(big.Int).SetBytes(input[32:64])
	l.set(allowedKey(holder), allowed)
	return nil, remaining, nil
}
