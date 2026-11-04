package settleledger

// The ERC-20 surface over the shared settlement ledger.
//
// Standard ERC-20 wants one address per token and a precompile has exactly one address,
// so the token identity here is the *caller*: a per-symbol shell contract calls these
// entry points and its own address is the token whose balances they read and write. A
// symbol therefore has a real address with code, an ABI, events and a name, while every
// balance stays in this one shared ledger, and the fast batch path never touches any of
// it.
//
// That also restores a distinction the keyed-by-address model had lost. Naming a token
// by an address used only as a map key means nothing called AAPL independently exists
// and the thing holding balances is the thing that settles. With the caller as the
// identity, AAPL is an instance and settling is a capability of this precompile, which
// is the split the Solidity design has.
//
// None of it needs authentication. A caller reaches exactly one token's ledger — its
// own — so there is nothing to spoof: the worst a rogue contract can do is move the
// balances of a token that is itself. Entry is by CALL: the host refuses a DELEGATECALL
// into a precompile under current rules, and where it does not, the contract address
// passed in resolves to the delegating contract, so its reads and writes land in that
// contract's own storage rather than in this ledger.
//
// Two properties of the batch path are absent here, and cannot be added without
// leaving the standard:
//
//   - There is no exactly-once. A row carries a journal reference and the batch path
//     records it in the handled bitmap, so a resubmitted row is skipped rather than
//     applied twice. ERC-20 has no field for a reference and no method that could take
//     one, so a repeated transfer is a second transfer. Anything that needs exactly-once
//     over this surface has to carry its own idempotency, above it.
//   - There is no delivery against payment. A transfer moves one asset one way; the
//     batch path moves a security leg and a cash leg together or moves neither. Two
//     transfers in one transaction are not the same thing: they can be interleaved with
//     other calls, and either can be the one that reverts.

import (
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
)

var (
	selErc20BalanceOf    = crypto.Keccak256([]byte("erc20BalanceOf(address)"))[:4]
	selErc20Entitlement  = crypto.Keccak256([]byte("erc20Entitlement(address)"))[:4]
	selErc20TotalSupply  = crypto.Keccak256([]byte("erc20TotalSupply()"))[:4]
	selErc20Allowance    = crypto.Keccak256([]byte("erc20Allowance(address,address)"))[:4]
	selErc20CanHold      = crypto.Keccak256([]byte("erc20CanHold(address)"))[:4]
	selErc20Approve      = crypto.Keccak256([]byte("erc20Approve(address,address,uint256)"))[:4]
	selErc20Transfer     = crypto.Keccak256([]byte("erc20Transfer(address,address,uint256)"))[:4]
	selErc20TransferFrom = crypto.Keccak256([]byte("erc20TransferFrom(address,address,address,uint256)"))[:4]
	selErc20Moves        = crypto.Keccak256([]byte("erc20Moves()"))[:4]
)

// Domains 10 to 12 continue the separator scheme in slot(): supply per token, allowance
// per (token, owner, spender), and one counter for movements made through this surface.
func supplyKey(token common.Address) common.Hash { return slot(10, token, common.Address{}) }

func allowanceKey(token, owner, spender common.Address) common.Hash {
	var buf [97]byte
	copy(buf[12:32], token[:])
	copy(buf[44:64], owner[:])
	copy(buf[76:96], spender[:])
	buf[96] = 11
	return common.BytesToHash(crypto.Keccak256(buf[:]))
}

// erc20MovesKey counts balance movements made through this surface, for the same reason
// seqKey counts settled rows: a rate has to be readable from the ledger rather than from
// what a driver believes it sent.
func erc20MovesKey() common.Hash { return slot(12, common.Address{}, common.Address{}) }

// An allowance of 2^256-1 is treated as unlimited and is not decremented, which is both
// the convention every ERC-20 integration expects and one fewer write per pull.
var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

func word(v *big.Int) []byte { return common.BigToHash(v).Bytes() }

func boolWord(b bool) []byte {
	if b {
		return word(big.NewInt(1))
	}
	return word(new(big.Int))
}

// erc20Read serves the view methods. The token is the caller, so a shell's balanceOf
// resolves to that shell's own ledger with no argument saying which token it means.
func erc20Read(
	need int, read func(l ledger, token common.Address, input []byte) *big.Int,
) contract.RunStatefulPrecompileFunc {
	return func(
		evm contract.AccessibleState, caller, addr common.Address, input []byte, suppliedGas uint64, _ bool,
	) ([]byte, uint64, error) {
		remaining, err := contract.DeductGas(suppliedGas, perCallGas)
		if err != nil {
			return nil, 0, err
		}
		if len(input) < need {
			return nil, remaining, vm.ErrExecutionReverted
		}
		l := ledger{state: evm.GetStateDB(), addr: addr}
		return word(read(l, caller, input)), remaining, nil
	}
}

// erc20BalanceOf answers omnibus custody, not party entitlement.
//
// That is the granularity RestrictedToken has: its balanceOf is an omnibus balance and
// only an allowed holder may have one, while party-level entitlement lives in the
// settlement contract's shadowBalance and was never an ERC-20 balance. Mapping the
// standard onto custody is what makes the two-granularity split survive the port
// instead of being flattened into a single number that is neither.
//
// What follows from it is worth stating rather than discovering. A sponsored party
// address reads zero here. That is the truth about it — it holds nothing, which is the
// entire point of sponsored access — but a wallet pointed at a party address shows an
// empty balance for a real position, and no ERC-20 method can ask the other question.
// erc20Entitlement answers it, off to the side of the standard where it belongs.
var runErc20BalanceOf = erc20Read(32, func(l ledger, token common.Address, in []byte) *big.Int {
	return l.get(heldKey(token, common.BytesToAddress(in[12:32])))
})

var runErc20Entitlement = erc20Read(32, func(l ledger, token common.Address, in []byte) *big.Int {
	return l.get(shadowKey(token, common.BytesToAddress(in[12:32])))
})

// Entitlement and custody are credited in equal amounts by credit(), so one counter is
// the exact total of both granularities and neither needs its own.
var runErc20TotalSupply = erc20Read(0, func(l ledger, token common.Address, _ []byte) *big.Int {
	return l.get(supplyKey(token))
})

var runErc20Allowance = erc20Read(64, func(l ledger, token common.Address, in []byte) *big.Int {
	return l.get(allowanceKey(token,
		common.BytesToAddress(in[12:32]), common.BytesToAddress(in[44:64])))
})

// erc20CanHold exists so the shell can say *why* a transfer failed. A precompile revert
// carries no data, so without this the shell could only report that the ledger refused,
// and RestrictedToken distinguishes "recipient may not hold" from "balance too small".
var runErc20CanHold = erc20Read(32, func(l ledger, token common.Address, in []byte) *big.Int {
	if l.canHold(common.BytesToAddress(in[12:32])) {
		return big.NewInt(1)
	}
	return new(big.Int)
})

var runErc20Moves = erc20Read(0, func(l ledger, _ common.Address, _ []byte) *big.Int {
	return l.get(erc20MovesKey())
})

// erc20Write wraps the three mutating methods: charge, refuse in a static call, then run.
func erc20Write(
	need int, apply func(l ledger, token common.Address, input []byte) bool,
) contract.RunStatefulPrecompileFunc {
	return func(
		evm contract.AccessibleState, caller, addr common.Address, input []byte, suppliedGas uint64, readOnly bool,
	) ([]byte, uint64, error) {
		remaining, err := contract.DeductGas(suppliedGas, perCallGas)
		if err != nil {
			return nil, 0, err
		}
		if readOnly {
			return nil, remaining, vm.ErrWriteProtection
		}
		if len(input) < need {
			return nil, remaining, vm.ErrExecutionReverted
		}
		l := ledger{state: evm.GetStateDB(), addr: addr}
		if !apply(l, caller, input) {
			// ERC-20 must fail loudly: SafeERC20 and every bridge built on it treat a
			// silent false as success. This is the opposite of the batch path, where a
			// row that cannot settle is skipped so it cannot cost the other rows theirs
			// — two different contracts of failure, each right for its caller.
			return nil, remaining, vm.ErrExecutionReverted
		}
		return boolWord(true), remaining, nil
	}
}

// The owner is whoever the shell says its msg.sender was. The shell can only ever name
// an owner within its own token's ledger, so a wrong answer costs it its own balances.
var runErc20Approve = erc20Write(96, func(l ledger, token common.Address, in []byte) bool {
	l.set(allowanceKey(token, common.BytesToAddress(in[12:32]), common.BytesToAddress(in[44:64])),
		new(big.Int).SetBytes(in[64:96]))
	return true
})

var runErc20Transfer = erc20Write(96, func(l ledger, token common.Address, in []byte) bool {
	return l.erc20Move(token, common.BytesToAddress(in[12:32]), common.BytesToAddress(in[44:64]),
		new(big.Int).SetBytes(in[64:96]))
})

var runErc20TransferFrom = erc20Write(128, func(l ledger, token common.Address, in []byte) bool {
	spender := common.BytesToAddress(in[12:32])
	from := common.BytesToAddress(in[44:64])
	to := common.BytesToAddress(in[76:96])
	amount := new(big.Int).SetBytes(in[96:128])

	k := allowanceKey(token, from, spender)
	allowed := l.get(k)
	if allowed.Cmp(amount) < 0 {
		return false
	}
	if !l.erc20Move(token, from, to, amount) {
		return false
	}
	if allowed.Cmp(maxUint256) != 0 {
		l.set(k, new(big.Int).Sub(allowed, amount))
	}
	return true
})

// erc20Move is the only place this surface changes a balance, so the rule that a barred
// holder cannot receive is stated once and no method added later can route around it:
// moveHeld checks it. That is the structural guarantee RestrictedToken gets from having
// a single _move, and it is why the check is here rather than in the shell — a shell is
// ordinary deployed code and there can be more than one of them per ledger.
//
// It moves custody and only custody. An ERC-20 transfer between omnibuses is a custody
// movement — a firm shifting its own inventory, or a bridge locking it — and party
// entitlement is a different granularity that settlement moves. Conflating them would
// mean any transfer silently re-attributed someone's position.
//
// The consequence, which is where this surface can weaken the ledger: custody can be
// transferred out of an omnibus while entitlement remains attributed under it, leaving
// that entitlement unbacked. Settlement then refuses the row — the custody leg finds no
// balance and the row is skipped — so nothing settles against backing that is not
// there, but the two granularities can disagree until someone looks. The Solidity
// design has the same exposure through the same door, an ERC-20 transfer by a holder.
func (l ledger) erc20Move(token, from, to common.Address, amount *big.Int) bool {
	if !l.moveHeld(token, from, to, amount) {
		return false
	}
	l.set(erc20MovesKey(), new(big.Int).Add(l.get(erc20MovesKey()), big.NewInt(1)))
	return true
}
