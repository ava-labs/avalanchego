package asmevm

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/holiman/uint256"
)

// One message run as coreth's eth_call runs it (SAE: libevm ethapi.DoCall, core.ApplyMessage
// under vm.Config{NoBaseFee: true} with a message that skips the account checks), on a fresh
// State over a Loader, under an SAE block's context as Chain.Exec builds it. The state is
// thrown away.

// cchainWarm is the C-Chain's warm precompile set of the SAE era (as the fixtures' extractor
// saw it).
var cchainWarm = []Address{{0x01}, {0x01, 19: 0x01}, {0x01, 19: 0x02}, {18: 0x01}, {19: 1}, {19: 2}, {19: 3}, {19: 4}, {19: 5}, {19: 6}, {19: 7}, {19: 8}, {19: 9}, {19: 10}}

func cchainAvalanche(networkID uint32, blockchainID Hash) *Avalanche {
	return &Avalanche{NetworkID: networkID, BlockchainID: blockchainID, Warm: cchainWarm}
}

// SAEHeader is what an SAE block's EVM context takes from the block.
type SAEHeader struct {
	Number, Time, GasLimit uint64
	Coinbase               Address
	BaseFee                uint256.Int // the execution base fee (the gas clock's), not the header's
}

// saeContext is the EVM context of an SAE block (today's rules: Rules nil), BLOCKHASH from
// hashes.
func saeContext(av *Avalanche, chainID uint64, h *SAEHeader, hashes map[uint64]Hash) BlockContext {
	return BlockContext{
		ChainID: chainID, Avalanche: av, Coinbase: h.Coinbase, GasLimit: h.GasLimit, BlockNumber: h.Number, Time: h.Time,
		BaseFee: h.BaseFee, BlobBaseFee: *uint256.NewInt(1), Random: Hash{31: 1}, BlockHashes: hashes,
	}
}

// loadAccount puts addr's loader account into st (nothing when the loader has none).
func loadAccount(st *State, l Loader, addr Address) error {
	ok, nonce, bal, ch, code, h, err := l.Account(addr)
	if err != nil || !ok {
		return err
	}
	a := newAccount()
	a.nonce, a.balance, a.bh = nonce, bal, h
	if mc, ok := l.(interface{ MultiCoin(Address) bool }); ok {
		a.multicoin = mc.MultiCoin(addr) // the sticky coreth flag: an empty multicoin account exists
	}
	if _, ok := st.base.(CoinBase); ok && a.multicoin { // coin balances only exist on flagged accounts
		a.mcAsked = map[Hash]bool{}
	}
	if len(code) > 0 {
		a.code, a.codeHash = code, ch
	}
	st.accounts[addr] = a
	return nil
}

// CChain names the chain an SAE block runs on (mainnet: network 1, the C-Chain's blockchain
// ID, chain ID 43114).
type CChain struct {
	NetworkID    uint32
	BlockchainID Hash
	ChainID      uint64
}

// Msg is an eth_call message as libevm's TransactionArgs.ToMessage builds it: Caller (zero
// when not given), To (nil: creation), Data, Value, Gas, AccessList, GasPrice (the price the
// EVM sees: the legacy price, min(tip + base fee, fee cap), or zero) and the fee caps.
// Tx.Nonce is unused (no nonce check).
type Msg struct {
	Tx
	FeeCap, TipCap uint256.Int
	// Dynamic: the caps were given as maxFeePerGas / maxPriorityFeePerGas; ApplyCall sets
	// GasPrice to min(TipCap + base fee, FeeCap), or zero when both caps are.
	Dynamic bool
}

// CallResult is what the message did.
type CallResult struct {
	Status  string // success | revert | halt | invalid (rejected before execution: Err says why)
	Output  []byte // the return data; the revert data on revert; nil on halt
	GasUsed uint64 // gross gas (before refunds)
	// Err is nil on success, ErrExecutionReverted on revert, the halt in libevm's words
	// (vm.ErrOutOfGas, ErrInvalidOpCode, ...), or the check that rejected the message, worded
	// as core.ApplyMessage words it (fee caps, balance, intrinsic gas, transfer, init code size).
	Err error
}

// ApplyCall runs m on the state of l under b's SAE context. hash gives BLOCKHASH of an
// earlier block on first use. The error is a failure of l or hash.
func ApplyCall(l Loader, c CChain, b *SAEHeader, hash func(n uint64) (Hash, error), m *Msg) (r CallResult, err error) {
	st := NewState()
	st.base = l
	seen := map[Address]bool{}
	st.OnMiss = func(addr Address, slot *Hash) {
		if slot != nil || seen[addr] {
			return
		}
		seen[addr] = true
		if err := loadAccount(st, l, addr); err != nil {
			panic(err)
		}
	}
	hashes := map[uint64]Hash{}
	blk := saeContext(cchainAvalanche(c.NetworkID, c.BlockchainID), c.ChainID, b, hashes)
	blk.OnHashMiss = func(n uint64) {
		h, err := hash(n)
		if err != nil {
			panic(err)
		}
		hashes[n] = h
	}
	if m.Dynamic && (!m.FeeCap.IsZero() || !m.TipCap.IsZero()) { // ToMessage's 1559 price
		p, over := new(uint256.Int).AddOverflow(&m.TipCap, &b.BaseFee)
		if over || p.Gt(&m.FeeCap) {
			p = &m.FeeCap
		}
		m.GasPrice = *p
	}
	if m.GasPrice.IsZero() { // vm.NewEVM under NoBaseFee: no price, no base fee
		blk.BaseFee.Clear()
	}
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("eth_call: %v", p)
			}
			r = CallResult{}
		}
	}()
	evm := NewEVM(blk, TxContext{}, st)
	evm.EnableAsm(false) // the Go loop words its halts as geth does (errInvalidOp, ...)
	if err := preCheck(evm, m); err != nil {
		return CallResult{Status: "invalid", Err: err}, nil
	}
	out, gross, _, xerr := evm.ExecTx(&m.Tx)
	r = CallResult{Status: "success", Output: out, GasUsed: gross, Err: gethErr(xerr)}
	switch {
	case xerr == ErrExecutionReverted:
		r.Status = "revert"
	case xerr != nil:
		r.Status, r.Output = "halt", nil
	}
	return r, nil
}

// preCheck is core.StateTransition's checks before execution as an eth_call meets them
// (preCheck without the account checks, buyGas, then transitionDb up to the call), in order.
func preCheck(evm *EVM, m *Msg) error {
	from := ChecksumHex(m.Caller)
	if !m.FeeCap.IsZero() || !m.TipCap.IsZero() { // NoBaseFee skips these for a call without prices
		if m.FeeCap.Lt(&m.TipCap) {
			return fmt.Errorf("max priority fee per gas higher than max fee per gas: address %s, maxPriorityFeePerGas: %s, maxFeePerGas: %s", from, m.TipCap.Dec(), m.FeeCap.Dec())
		}
		if m.FeeCap.Lt(&evm.Block.BaseFee) {
			return fmt.Errorf("max fee per gas less than block base fee: address %s, maxFeePerGas: %s, baseFee: %s", from, m.FeeCap.Dec(), evm.Block.BaseFee.Dec())
		}
	}
	gas := uint256.NewInt(m.Gas)
	want, of1 := new(uint256.Int).MulOverflow(gas, &m.FeeCap)
	_, of2 := want.AddOverflow(want, &m.Value)
	if of1 || of2 {
		return fmt.Errorf("insufficient funds for gas * price + value: address %s required balance exceeds 256 bits", from)
	}
	have := evm.state.Balance(m.Caller)
	if have.Lt(want) {
		return fmt.Errorf("insufficient funds for gas * price + value: address %s have %s want %s", from, have.Dec(), want.Dec())
	}
	intrinsic, err := evm.intrinsic(&m.Tx)
	if err != nil {
		return err
	}
	if m.Gas < intrinsic {
		return fmt.Errorf("intrinsic gas too low: have %d, want %d", m.Gas, intrinsic)
	}
	left := new(uint256.Int).Sub(&have, new(uint256.Int).Mul(gas, &m.GasPrice)) // after buyGas
	if !m.Value.IsZero() && left.Lt(&m.Value) {
		return fmt.Errorf("insufficient funds for transfer: address %s", from)
	}
	if m.To == nil && len(m.Data) > MaxInitCodeSize {
		return fmt.Errorf("max initcode size exceeded: code size %d limit %d", len(m.Data), MaxInitCodeSize)
	}
	return nil
}

// gethErr words a frame error as libevm's vm and precompiles do where asmevm's own words
// differ (the Go loop's halts already carry geth's detail: errInvalidOp, errUnderflow,
// errOverflow).
func gethErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errBlake2FInputLength), errors.Is(err, errKZGInputLength):
		return errors.New("invalid input length")
	case errors.Is(err, errBlake2FFinalFlag):
		return errors.New("invalid final flag")
	case errors.Is(err, errBn256PairingLength):
		return errors.New("bad elliptic curve pairing size")
	case errors.Is(err, errKZGVersionedHash):
		return errors.New("mismatched versioned hash")
	case errors.Is(err, errKZGProof):
		return errors.New("error verifying kzg proof")
	}
	return err
}

// ChecksumHex is addr in EIP-55 mixed case with 0x (geth's common.Address.Hex).
func ChecksumHex(addr Address) string {
	h := []byte(hex.EncodeToString(addr[:]))
	k := Keccak256(h)
	for i, c := range h {
		if c > '9' && k[i/2]>>(4*(1-uint(i)%2))&0xf > 7 {
			h[i] = c - 32
		}
	}
	return "0x" + string(h)
}
