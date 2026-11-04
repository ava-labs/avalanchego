package asmevm

import (
	"fmt"
	"sort"

	"github.com/holiman/uint256"
)

// Tx is one transaction to execute against a State.
type Tx struct {
	Caller     Address
	To         *Address // nil = contract creation
	Data       []byte
	Value      uint256.Int
	Gas        uint64
	Nonce      uint64
	GasPrice   uint256.Int
	AccessList []AccessTuple
}

// AccessTuple is one EIP-2930 access list entry.
type AccessTuple struct {
	Address Address
	Keys    []Hash
}

// ExecResult is the outcome of a transaction, in revm canonical shape.
type ExecResult struct {
	Status    string // success | revert | halt
	Output    []byte
	GasUsed   uint64 // net (after refund)
	GasGross  uint64
	GasRefund uint64
	Logs      []Log
	Storage   []StorageChange
	Accounts  []AccountChange
}

type StorageChange struct {
	Address Address
	Key     Hash
	Value   Hash
}

type AccountChange struct {
	Address Address
	Balance uint256.Int
	Nonce   uint64
}

// intrinsicGas returns the base gas for a call transaction's calldata.
func intrinsicGas(data []byte, isCreate bool) uint64 {
	var gas uint64 = TxGas
	if isCreate {
		gas = TxGasContractCreation
	}
	var nz uint64
	for _, b := range data {
		if b != 0 {
			nz++
		}
	}
	gas += nz * TxDataNonZeroGas
	gas += (uint64(len(data)) - nz) * TxDataZeroGas
	if isCreate {
		gas += toWordSize(uint64(len(data))) * InitCodeWordGas
	}
	return gas
}

// InitialState holds account balances/nonces before a transaction, for the
// account-change diff of ResultAfter. Treat it as immutable after capture.
type InitialState map[Address]initialAccount

type initialAccount struct {
	bal   uint256.Int
	nonce uint64
}

// CaptureInitial snapshots account balances/nonces before transaction execution.
func (evm *EVM) CaptureInitial() InitialState {
	initial := make(InitialState, len(evm.state.accounts))
	for addr, a := range evm.state.accounts {
		initial[addr] = initialAccount{a.balance, a.nonce}
	}
	return initial
}

// RunTx executes tx against the EVM's state and returns the canonical result.
func (evm *EVM) RunTx(tx *Tx) ExecResult {
	initial := evm.CaptureInitial()
	out, gross, net, err := evm.ExecTx(tx)
	return evm.ResultAfter(initial, out, gross, net, err)
}

// RunTxReceipt executes tx and returns only the receipt fields (status,
// output, gas, refund, logs): no state change lists, no O(state) walk.
func (evm *EVM) RunTxReceipt(tx *Tx) ExecResult {
	out, gross, net, err := evm.ExecTx(tx)
	return evm.receiptResult(out, gross, net, err)
}

func (evm *EVM) receiptResult(out []byte, gross, net uint64, err error) ExecResult {
	status := "success"
	if err == ErrExecutionReverted {
		status = "revert"
	} else if err != nil {
		status = "halt"
		out = nil
	}
	return ExecResult{Status: status, Output: out, GasUsed: net, GasGross: gross, GasRefund: gross - net, Logs: evm.state.logs}
}

// ResultAfter extracts the canonical result (status, output, gas, refund, logs,
// storage and account changes) of an ExecTx that already ran on this EVM's
// state. It executes nothing; call it before the state is modified again.
func (evm *EVM) ResultAfter(initial InitialState, out []byte, gross, net uint64, err error) ExecResult {
	st := evm.state
	res := evm.receiptResult(out, gross, net, err)

	// Storage changes vs tx-start committed values, sorted by address then key.
	for i := range st.orig.e {
		o := &st.orig.e[i]
		a := st.accounts[o.k.addr]
		if o.gen != st.orig.gen || o.dead || a == nil { // nil: removed at the end of the tx (EIP-6780)
			continue
		}
		v, _ := st.stored(a, &o.k.slot)
		if v.Eq(&o.v) {
			continue
		}
		res.Storage = append(res.Storage, StorageChange{Address: o.k.addr, Key: o.k.slot, Value: Hash(v.Bytes32())})
	}
	sort.Slice(res.Storage, func(i, j int) bool {
		if res.Storage[i].Address != res.Storage[j].Address {
			return addrLess(res.Storage[i].Address, res.Storage[j].Address)
		}
		return hashLess(res.Storage[i].Key, res.Storage[j].Key)
	})

	// Account changes (balance or nonce differ from initial), sorted by address.
	for addr, a := range st.accounts {
		in0, had := initial[addr]
		if had && a.balance.Eq(&in0.bal) && a.nonce == in0.nonce {
			continue
		}
		if !had && a.balance.IsZero() && a.nonce == 0 {
			continue
		}
		res.Accounts = append(res.Accounts, AccountChange{Address: addr, Balance: a.balance, Nonce: a.nonce})
	}
	sort.Slice(res.Accounts, func(i, j int) bool {
		return addrLess(res.Accounts[i].Address, res.Accounts[j].Address)
	})
	if res.Logs == nil {
		res.Logs = []Log{}
	}
	return res
}

func addrLess(a, b Address) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
func hashLess(a, b Hash) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// intrinsic is tx's intrinsic gas under the EVM's rules: calldata, creation (EIP-3860 init
// code words from Shanghai), the access list, whose warp entries are predicates (their gas
// replaces the access-list gas). err: an invalid warp predicate.
func (evm *EVM) intrinsic(tx *Tx) (uint64, error) {
	isCreate := tx.To == nil
	g := intrinsicGas(tx.Data, isCreate)
	if isCreate && !evm.Block.Rules.shanghai() {
		g -= toWordSize(uint64(len(tx.Data))) * InitCodeWordGas
	}
	for i := range tx.AccessList {
		e := &tx.AccessList[i]
		if evm.isWarpPredicate(e) {
			pg, err := warpPredicateGas(e.Keys, warpGasFor(evm.Block.Rules))
			if err != nil {
				return 0, err
			}
			g += pg
			continue
		}
		g += TxAccessListAddressGas + TxAccessListStorageKeyGas*uint64(len(e.Keys))
	}
	return g, nil
}

func (evm *EVM) isWarpPredicate(e *AccessTuple) bool {
	return evm.Block.Avalanche != nil && e.Address == warpAddr && (evm.Block.Rules == nil || evm.Block.Rules.Durango)
}

// ExecTx executes tx (the timed part: no result diffing). It returns the
// top-level output, gross and net (after refund) gas, and the frame error.
func (evm *EVM) ExecTx(tx *Tx) (out []byte, gross, net uint64, err error) {
	st := evm.state
	st.beginTx()
	evm.interp.resetKeccakCache()
	evm.Tx.GasPrice = tx.GasPrice
	evm.Tx.Origin = tx.Caller

	// Buy gas upfront.
	gasFee := new(uint256.Int).Mul(&tx.GasPrice, new(uint256.Int).SetUint64(tx.Gas))
	st.SubBalance(tx.Caller, gasFee)
	if tx.To != nil { // CREATE bumps the sender nonce itself
		st.SetNonce(tx.Caller, st.Nonce(tx.Caller)+1)
	}

	// Warm initial access list: caller, to, coinbase, precompiles 1..10.
	st.warmAtStart(tx.Caller)
	if tx.To != nil {
		st.warmAtStart(*tx.To)
	}
	rules := evm.Block.Rules
	if rules.shanghai() { // EIP-3651
		st.warmAtStart(evm.Block.Coinbase)
	}
	av := evm.Block.Avalanche
	if rules != nil {
		for _, p := range activePrecompiles(rules) {
			st.warmAtStart(p)
		}
	} else if av != nil {
		for _, p := range av.Warm {
			st.warmAtStart(p)
		}
	} else {
		for i := byte(1); i <= 10; i++ {
			var p Address
			p[19] = i
			st.warmAtStart(p)
		}
	}

	isCreate := tx.To == nil
	intrinsic, perr := evm.intrinsic(tx)
	if perr != nil {
		panic(fmt.Sprintf("invalid warp predicate (the chain rejects this tx): %v", perr))
	}
	evm.warpPreds = nil
	for _, e := range tx.AccessList {
		st.warmAtStart(e.Address)
		for _, k := range e.Keys {
			st.accessSlot(e.Address, k)
		}
		if evm.isWarpPredicate(&e) {
			evm.warpPreds = append(evm.warpPreds, e.Keys)
		}
	}

	var leftover uint64
	if tx.Gas < intrinsic {
		leftover = 0
		err = ErrOutOfGas
	} else {
		gasRemaining := tx.Gas - intrinsic
		if isCreate {
			out, _, leftover, err = evm.Create(tx.Caller, tx.Data, gasRemaining, &tx.Value) // the output is the deployed code
		} else {
			out, leftover, err = evm.Call(tx.Caller, *tx.To, tx.Data, gasRemaining, &tx.Value)
		}
	}

	gross = tx.Gas - leftover
	refund := st.refund
	if maxRefund := gross / RefundQuotientEIP3529; refund > maxRefund {
		refund = maxRefund
	}
	if rules != nil { // coreth state_transition.go refundGas: half the gas used, none from AP1
		refund = min(st.refund, gross/2)
		if rules.AP1 {
			refund = 0
		}
	}
	net = gross - refund

	// Refund leftover + refund to caller; pay coinbase the tip.
	refundWei := new(uint256.Int).Mul(&tx.GasPrice, new(uint256.Int).SetUint64(tx.Gas-net))
	st.AddBalance(tx.Caller, refundWei)
	tip := new(uint256.Int).Sub(&tx.GasPrice, &evm.Block.BaseFee)
	if rules != nil { // coreth credits the whole fee (base fee included) to the coinbase
		tip = &tx.GasPrice
		st.AddBalance(evm.Block.Coinbase, new(uint256.Int).Mul(tip, new(uint256.Int).SetUint64(net)))
		st.endTxLegacy()
		return out, gross, net, err
	}
	if !tip.IsZero() {
		coinFee := new(uint256.Int).Mul(tip, new(uint256.Int).SetUint64(net))
		st.AddBalance(evm.Block.Coinbase, coinFee)
	}

	// EIP-6780: accounts created and self-destructed in this tx are removed.
	for _, t := range st.txAccts {
		if t.a.destroyed && st.accounts[t.addr] == t.a {
			st.forgetIndexed(t.addr)
			delete(st.accounts, t.addr)
		}
	}
	return out, gross, net, err
}
