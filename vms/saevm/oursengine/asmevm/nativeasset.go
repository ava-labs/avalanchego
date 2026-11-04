package asmevm

import (
	"fmt"

	"github.com/holiman/uint256"
)

// Native asset precompiles of ApricotPhase2 to Banff (coreth nativeasset/contract.go wrapped by
// params/hooks_libevm.go makePrecompile): 0x0100..01 reads a multicoin balance, 0x0100..02
// moves one and calls the recipient.

const (
	assetBalanceApricot = 2100
	assetCallApricot    = 20000
)

func coinKey(id []byte) (k Hash) {
	copy(k[:], id)
	k[0] |= 1 // normalizeCoinID
	return k
}

type nativeAssetBalance struct{}

func (nativeAssetBalance) requiredGas([]byte) uint64  { return 0 }
func (nativeAssetBalance) run([]byte) ([]byte, error) { return nil, ErrExecutionReverted }

// call: input = address (20 bytes) | asset id (32 bytes); output the balance as a word.
func (nativeAssetBalance) call(evm *EVM, _ Address, input []byte, gas uint64, _ callKind) ([]byte, uint64, error) {
	evm.state.coinUse = true
	if gas < assetBalanceApricot {
		return nil, 0, ErrOutOfGas
	}
	gas -= assetBalanceApricot
	if len(input) != 52 {
		return nil, gas, ErrExecutionReverted
	}
	v := evm.state.MultiCoin(Address(input[:20]), coinKey(input[20:52]))
	b := v.Bytes32()
	return b[:], gas, nil
}

type nativeAssetCall struct{}

func (nativeAssetCall) requiredGas([]byte) uint64  { return 0 }
func (nativeAssetCall) run([]byte) ([]byte, error) { return nil, ErrExecutionReverted }

// call: input = to (20) | asset id (32) | amount (32) | call data. The asset moves from the
// precompile's caller to `to`, then `to` is called with the caller's address as its caller
// (libevm WithUNSAFECallerAddressProxying) and all remaining gas, no 63/64 rule, no stipend, no
// access charge (WithLegacyOutboundCallGas).
func (nativeAssetCall) call(evm *EVM, caller Address, input []byte, gas uint64, kind callKind) ([]byte, uint64, error) {
	evm.state.coinUse = true
	if kind == kindDelegate || kind == kindCallCode {
		// libevm proxies a different caller for these (the delegating account) than the one
		// coreth debits (EVMSemantic.Caller); not ported, so the run stops here.
		panic(fmt.Sprintf("block %d: nativeAssetCall through DELEGATECALL/CALLCODE: not ported", evm.Block.BlockNumber))
	}
	if gas < assetCallApricot {
		return nil, 0, ErrOutOfGas
	}
	gas -= assetCallApricot
	if kind == kindStatic || evm.readOnly {
		return nil, gas, ErrExecutionReverted
	}
	if len(input) < 84 {
		return nil, gas, ErrExecutionReverted
	}
	st := evm.state
	to := Address(input[:20])
	key := coinKey(input[20:52])
	var amt uint256.Int
	amt.SetBytes(input[52:84])
	if !amt.IsZero() {
		if bal := st.MultiCoin(caller, key); bal.Lt(&amt) {
			return nil, gas, ErrInsufficientBalance
		}
	}
	snap := st.snapshot()
	if !st.Exists(to) {
		if gas < CallNewAccountGas {
			return nil, 0, ErrOutOfGas
		}
		gas -= CallNewAccountGas
		st.getOrNew(to)
	}
	if !amt.IsZero() {
		from := st.MultiCoin(caller, key)
		st.SetMultiCoin(caller, key, *from.Sub(&from, &amt))
		cur := st.MultiCoin(to, key)
		st.SetMultiCoin(to, key, *cur.Add(&cur, &amt))
	} else {
		st.AddBalance(to, new(uint256.Int)) // coreth AddBalanceMultiCoin of 0: a touch
	}
	// `to` runs without entering the access list (coreth evm.Call, not a CALL opcode); its
	// first slot access adds it (State.accessSlot), so a later call back into it is warm.
	evm.coldSelf++
	ret, left, err := evm.Call(caller, to, input[84:], gas, new(uint256.Int))
	evm.coldSelf--
	if err != nil {
		st.revert(snap)
		if err != ErrExecutionReverted {
			left = 0
		}
	}
	return ret, left, err
}
