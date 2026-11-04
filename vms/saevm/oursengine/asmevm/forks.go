package asmevm

import (
	"math/big"
	"slices"
	"sync"
	"time"

	"github.com/holiman/uint256"
)

// Rules selects the C-Chain fork of one block (coreth params/extras + params/config_extra.go
// at avalanchego 7e7ce92). BlockContext.Rules nil means today's rules (Cancun + Granite), the
// behavior the tier0 Chain and the fixtures use. The Ethereum forks follow coreth's mapping:
// Istanbul at genesis, Berlin and London at the AP2 and AP3 activation blocks (by number),
// Shanghai at Durango and Cancun at Etna (by time).
type Rules struct {
	AP1, AP2, AP3, AP4, AP5, Pre6, AP6, Post6, Banff, Cortina, Durango, Etna, Fortuna, Granite, Helicon bool
	Berlin, London, Shanghai, Cancun                                                                    bool
}

// Mainnet activation times and heights.
var (
	mainnetAP1      = utc(2021, time.March, 31, 14, 0)
	mainnetAP2      = utc(2021, time.May, 10, 11, 0)
	mainnetAP3      = utc(2021, time.August, 24, 14, 0)
	mainnetAP4      = utc(2021, time.September, 22, 21, 0)
	mainnetAP5      = utc(2021, time.December, 2, 18, 0)
	mainnetPre6     = time.Date(2022, time.September, 5, 1, 30, 0, 0, time.UTC).Unix()
	mainnetAP6      = utc(2022, time.September, 6, 20, 0)
	mainnetPost6    = utc(2022, time.September, 7, 3, 0)
	mainnetBanff    = utc(2022, time.October, 18, 16, 0)
	mainnetCortina  = utc(2023, time.April, 25, 15, 0)
	mainnetDurango  = utc(2024, time.March, 6, 16, 0)
	mainnetEtna     = utc(2024, time.December, 16, 17, 0)
	mainnetFortuna  = utc(2025, time.April, 8, 15, 0)
	mainnetGranite  = utc(2025, time.November, 19, 16, 0)
	mainnetHelicon  = utc(2026, time.September, 22, 15, 0)
	mainnetBerlinAt = uint64(1640340) // AP2 activation block
	mainnetLondonAt = uint64(3308552) // AP3 activation block
)

func utc(y int, m time.Month, d, h, min int) int64 {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC).Unix()
}

// MainnetRules is the rule set of the mainnet C-Chain block with this number and time.
func MainnetRules(number, t uint64) *Rules {
	at := func(u int64) bool { return int64(t) >= u }
	return &Rules{
		AP1: at(mainnetAP1), AP2: at(mainnetAP2), AP3: at(mainnetAP3), AP4: at(mainnetAP4), AP5: at(mainnetAP5),
		Pre6: at(mainnetPre6), AP6: at(mainnetAP6), Post6: at(mainnetPost6), Banff: at(mainnetBanff),
		Cortina: at(mainnetCortina), Durango: at(mainnetDurango), Etna: at(mainnetEtna), Fortuna: at(mainnetFortuna),
		Granite: at(mainnetGranite), Helicon: at(mainnetHelicon),
		Berlin: number >= mainnetBerlinAt, London: number >= mainnetLondonAt,
		Shanghai: at(mainnetDurango), Cancun: at(mainnetEtna),
	}
}

// Name is the newest fork in effect.
func (r *Rules) Name() string {
	switch {
	case r.Helicon:
		return "Helicon"
	case r.Granite:
		return "Granite"
	case r.Fortuna:
		return "Fortuna"
	case r.Etna:
		return "Etna"
	case r.Durango:
		return "Durango"
	case r.Cortina:
		return "Cortina"
	case r.Banff:
		return "Banff"
	case r.Post6:
		return "ApricotPhasePost6"
	case r.AP6:
		return "ApricotPhase6"
	case r.Pre6:
		return "ApricotPhasePre6"
	case r.AP5:
		return "ApricotPhase5"
	case r.AP4:
		return "ApricotPhase4"
	case r.AP3:
		return "ApricotPhase3"
	case r.AP2:
		return "ApricotPhase2"
	case r.AP1:
		return "ApricotPhase1"
	}
	return "Launch"
}

// cancun reports today's EVM (nil rules) or a Cancun block.
func (r *Rules) cancun() bool   { return r == nil || r.Cancun }
func (r *Rules) shanghai() bool { return r == nil || r.Shanghai }
func (r *Rules) london() bool   { return r == nil || r.London }
func (r *Rules) berlin() bool   { return r == nil || r.Berlin }

var (
	forkTablesOnce sync.Once
	istanbulTable  *[256]operation
	berlinTable    *[256]operation
	londonTable    *[256]operation
	shanghaiTable  *[256]operation
)

// tableFor is the instruction set of the rules (geth's jump tables by fork).
func tableFor(r *Rules) *[256]operation {
	if r.cancun() {
		return cancunInstructionSet()
	}
	forkTablesOnce.Do(buildForkTables)
	switch {
	case r.Shanghai:
		return shanghaiTable
	case r.London:
		return londonTable
	case r.Berlin:
		return berlinTable
	}
	return istanbulTable
}

func buildForkTables() {
	c := cancunInstructionSet()
	undef := operation{execute: opUndefined, undefined: true}

	sh := *c // Shanghai: Cancun without EIP-1153, EIP-5656, EIP-4844, EIP-7516 and with the pre-6780 SELFDESTRUCT
	for _, op := range []OpCode{TLOAD, TSTORE, MCOPY, BLOBHASH, BLOBBASEFEE} {
		sh[op] = undef
	}
	sh[SELFDESTRUCT].execute = opSelfdestructLegacy
	shanghaiTable = &sh

	lo := sh // London: no PUSH0 (EIP-3855), no init code metering (EIP-3860)
	lo[PUSH0] = undef
	lo[CREATE].dynamicGas = gasCreateLegacy
	lo[CREATE2].dynamicGas = gasCreate2Legacy
	londonTable = &lo

	be := lo // Berlin: no BASEFEE (EIP-3198)
	be[BASEFEE] = undef
	berlinTable = &be

	is := be // Istanbul: no EIP-2929 access costs (EIP-1884 / EIP-2200 / EIP-150 prices)
	is[SLOAD].constantGas, is[SLOAD].dynamicGas = 800, nil
	is[SSTORE].dynamicGas = gasSStoreEIP2200
	is[BALANCE].constantGas, is[BALANCE].dynamicGas = 700, nil
	is[EXTCODESIZE].constantGas, is[EXTCODESIZE].dynamicGas = 700, nil
	is[EXTCODEHASH].constantGas, is[EXTCODEHASH].dynamicGas = 700, nil
	is[EXTCODECOPY].constantGas, is[EXTCODECOPY].dynamicGas = 700, makeGasCopy(3)
	is[CALL].constantGas, is[CALL].dynamicGas = 700, gasCallLegacy
	is[CALLCODE].constantGas, is[CALLCODE].dynamicGas = 700, gasCallCodeLegacy
	is[DELEGATECALL].constantGas, is[DELEGATECALL].dynamicGas = 700, gasDelegateOrStaticLegacy
	is[STATICCALL].constantGas, is[STATICCALL].dynamicGas = 700, gasDelegateOrStaticLegacy
	is[SELFDESTRUCT].dynamicGas = gasSelfdestructLegacy
	istanbulTable = &is
}

// EIP-2200 SSTORE (Istanbul) constants.
const (
	sloadGasEIP2200           = 800
	sstoreClearsRefundEIP2200 = 15000
	selfdestructRefund        = 24000
)

// gasSStoreEIP2200 is geth's gasSStoreEIP2200. Before AP1 coreth read the original value
// without its state key normalization (core/evm.go StateDBAP0): for a slot whose first byte
// has bit 0 set the original is the raw key's value, which no EVM write ever stores (they go
// to the normalized key), so it is the multicoin balance of that coin id or zero.
func gasSStoreEIP2200(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	if c.Gas <= CallStipend {
		return 0, ErrOutOfGas
	}
	slot := Hash(stack.back(0).Bytes32())
	value := *stack.back(1)
	current := evm.state.GetStorage(c.self, slot)
	if current.Eq(&value) {
		return sloadGasEIP2200, nil
	}
	var original uint256.Int
	if r := evm.Block.Rules; r != nil && !r.AP1 && slot[0]&1 == 1 {
		original = evm.state.MultiCoin(c.self, slot)
	} else {
		original = evm.state.committedStorage(c.self, slot)
	}
	if original.Eq(&current) {
		if original.IsZero() {
			return SstoreSetGas, nil
		}
		if value.IsZero() {
			evm.state.addRefund(sstoreClearsRefundEIP2200)
		}
		return SstoreResetGas, nil
	}
	if !original.IsZero() {
		if current.IsZero() {
			evm.state.subRefund(sstoreClearsRefundEIP2200)
		} else if value.IsZero() {
			evm.state.addRefund(sstoreClearsRefundEIP2200)
		}
	}
	if original.Eq(&value) {
		if original.IsZero() {
			evm.state.addRefund(SstoreSetGas - sloadGasEIP2200)
		} else {
			evm.state.addRefund(SstoreResetGas - sloadGasEIP2200)
		}
	}
	return sloadGasEIP2200, nil
}

// gasCallLegacy is geth's gasCall (EIP-150 base 700 is the constant gas).
func gasCallLegacy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	transfersValue := !stack.back(2).IsZero()
	if transfersValue && evm.state.Empty(Address(stack.back(1).Bytes20())) {
		gas += CallNewAccountGas
	}
	if transfersValue {
		gas += CallValueTransferGas
	}
	return legacyCallGas(evm, c, stack, gas)
}

func gasCallCodeLegacy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	if !stack.back(2).IsZero() {
		gas += CallValueTransferGas
	}
	return legacyCallGas(evm, c, stack, gas)
}

func gasDelegateOrStaticLegacy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	return legacyCallGas(evm, c, stack, gas)
}

func legacyCallGas(evm *EVM, c *Contract, stack *Stack, gas uint64) (uint64, error) {
	var err error
	evm.callGasTemp, err = callGas(c.Gas, gas, stack.back(0))
	if err != nil {
		return 0, err
	}
	total, overflow := safeAdd(gas, evm.callGasTemp)
	if overflow {
		return 0, ErrGasUintOverflow
	}
	return total, nil
}

// gasSelfdestructLegacy is geth's EIP-150/158 SELFDESTRUCT gas with the pre-London refund.
func gasSelfdestructLegacy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	var gas uint64
	if bal := evm.state.Balance(c.self); !bal.IsZero() && evm.state.Empty(Address(stack.back(0).Bytes20())) {
		gas += CallNewAccountGas
	}
	if !evm.state.destroyed(c.self) {
		evm.state.addRefund(selfdestructRefund)
	}
	return gas, nil
}

// gasCreateLegacy and gasCreate2Legacy are CREATE and CREATE2 before EIP-3860.
func gasCreateLegacy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	return memoryGasCost(mem, memorySize)
}

func gasCreate2Legacy(evm *EVM, c *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return 0, err
	}
	size, overflow := stack.back(2).Uint64WithOverflow()
	if overflow {
		return 0, ErrGasUintOverflow
	}
	wordGas, o := safeMul(toWordSize(size), Keccak256WordGas)
	if o {
		return 0, ErrGasUintOverflow
	}
	if gas, overflow = safeAdd(gas, wordGas); overflow {
		return 0, ErrGasUintOverflow
	}
	return gas, nil
}

// opSelfdestructLegacy is SELFDESTRUCT before EIP-6780: the balance moves to the beneficiary
// (burned when it is the account itself) and the account is deleted at the end of the tx.
func opSelfdestructLegacy(pc *uint64, in *Interpreter, scope *scopeContext) ([]byte, error) {
	if in.readOnly {
		return nil, ErrWriteProtection
	}
	b := scope.Stack.pop()
	ben := Address(b.Bytes20())
	self := scope.Contract.self
	bal := in.evm.state.Balance(self)
	in.evm.state.AddBalance(ben, &bal)
	left := in.evm.state.Balance(self)
	in.evm.state.SubBalance(self, &left)
	in.evm.state.markDestroyed(self)
	return nil, errStopToken
}

// modexpGasEIP198 is the modexp price before EIP-2565 (Byzantium to Berlin).
func modexpGasEIP198(input []byte) uint64 {
	baseLen := new(big.Int).SetBytes(getData(input, 0, 32))
	expLen := new(big.Int).SetBytes(getData(input, 32, 32))
	modLen := new(big.Int).SetBytes(getData(input, 64, 32))
	if len(input) > 96 {
		input = input[96:]
	} else {
		input = nil
	}
	big32 := big.NewInt(32)
	expHead := new(big.Int)
	if big.NewInt(int64(len(input))).Cmp(baseLen) > 0 {
		n := uint64(32)
		if expLen.Cmp(big32) <= 0 {
			n = expLen.Uint64()
		}
		expHead.SetBytes(getData(input, baseLen.Uint64(), n))
	}
	adjExpLen := new(big.Int)
	if expLen.Cmp(big32) > 0 {
		adjExpLen.Sub(expLen, big32)
		adjExpLen.Lsh(adjExpLen, 3)
	}
	if bl := expHead.BitLen(); bl > 0 {
		adjExpLen.Add(adjExpLen, big.NewInt(int64(bl-1)))
	}
	if adjExpLen.Sign() == 0 {
		adjExpLen.SetInt64(1)
	}
	x := baseLen
	if modLen.Cmp(baseLen) > 0 {
		x = modLen
	}
	// geth modexpMultComplexity (EIP-198)
	gas := new(big.Int)
	switch {
	case x.Cmp(big.NewInt(64)) <= 0:
		gas.Mul(x, x)
	case x.Cmp(big.NewInt(1024)) <= 0:
		gas.Add(new(big.Int).Div(new(big.Int).Mul(x, x), big.NewInt(4)), new(big.Int).Sub(new(big.Int).Mul(big.NewInt(96), x), big.NewInt(3072)))
	default:
		gas.Add(new(big.Int).Div(new(big.Int).Mul(x, x), big.NewInt(16)), new(big.Int).Sub(new(big.Int).Mul(big.NewInt(480), x), big.NewInt(199680)))
	}
	gas.Mul(gas, adjExpLen)
	gas.Div(gas, big.NewInt(20))
	if gas.BitLen() > 64 {
		return ^uint64(0)
	}
	return gas.Uint64()
}

type modexpEIP198 struct{ modexp }

func (modexpEIP198) requiredGas(input []byte) uint64 { return modexpGasEIP198(input) }

// activePrecompiles is coreth's ActivePrecompiles for the rules: the Ethereum set of the fork
// plus the Avalanche ones of params/hooks_libevm.go currentPrecompiles (warm from Berlin).
func activePrecompiles(r *Rules) []Address {
	i := 0
	if r.Cancun {
		i |= 1
	}
	if r.AP2 {
		i |= 2
	}
	if r.Granite {
		i |= 4
	}
	return precompileSets[i] // read only: built once, not per tx
}

// precompileSets are the precompile address lists by Cancun | AP2<<1 | Granite<<2.
var precompileSets = func() (t [8][]Address) {
	for i := range t {
		n := byte(9)
		if i&1 != 0 {
			n = 10
		}
		for a := byte(1); a <= n; a++ {
			t[i] = append(t[i], Address{19: a})
		}
		if i&2 != 0 {
			t[i] = append(t[i], Address{0: 1}, Address{0: 1, 19: 1}, Address{0: 1, 19: 2})
		}
		if i&4 != 0 {
			t[i] = append(t[i], p256Addr)
		}
		t[i] = slices.Clip(t[i])
	}
	return t
}()
