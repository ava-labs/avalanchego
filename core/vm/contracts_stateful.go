package vm

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// PrecompiledContractsApricot contains the default set of pre-compiled Ethereum
// contracts used in the Istanbul release and the stateful precompiled contracts
// added for the Avalanche Apricot release.
// Apricot is incompatible with the YoloV3 Release since it does not include the
// BLS12-381 Curve Operations added to the set of precompiled contracts

var (
	genesisMulticoinContractAddr = common.HexToAddress("0x0100000000000000000000000000000000000000")
	nativeAssetBalanceAddr       = common.HexToAddress("0x0100000000000000000000000000000000000001")
	nativeAssetCallAddr          = common.HexToAddress("0x0100000000000000000000000000000000000002")
)

var PrecompiledContractsApricot = map[common.Address]StatefulPrecompiledContract{
	common.BytesToAddress([]byte{1}): newWrappedPrecompiledContract(&ecrecover{}),
	common.BytesToAddress([]byte{2}): newWrappedPrecompiledContract(&sha256hash{}),
	common.BytesToAddress([]byte{3}): newWrappedPrecompiledContract(&ripemd160hash{}),
	common.BytesToAddress([]byte{4}): newWrappedPrecompiledContract(&dataCopy{}),
	common.BytesToAddress([]byte{5}): newWrappedPrecompiledContract(&bigModExp{}),
	common.BytesToAddress([]byte{6}): newWrappedPrecompiledContract(&bn256AddIstanbul{}),
	common.BytesToAddress([]byte{7}): newWrappedPrecompiledContract(&bn256ScalarMulIstanbul{}),
	common.BytesToAddress([]byte{8}): newWrappedPrecompiledContract(&bn256PairingIstanbul{}),
	common.BytesToAddress([]byte{9}): newWrappedPrecompiledContract(&blake2F{}),
	genesisMulticoinContractAddr:     &genesisContract{},
	nativeAssetBalanceAddr:           &nativeAssetBalance{gasCost: params.AssetBalanceApricot},
	nativeAssetCallAddr:              &nativeAssetCall{gasCost: params.AssetCallApricot},
}

// StatefulPrecompiledContract is the interface for executing a precompiled contract
// This wraps the PrecompiledContracts native to Ethereum and allows adding in stateful
// precompiled contracts to support native Avalanche asset transfers.
type StatefulPrecompiledContract interface {
	// Run executes a precompiled contract in the current state
	// assumes that it has already been verified that [caller] can
	// transfer [value].
	Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error)
}

// wrappedPrecompiledContract implements StatefulPrecompiledContract by wrapping stateless native precompiled contracts
// in Ethereum.
type wrappedPrecompiledContract struct {
	p PrecompiledContract
}

func newWrappedPrecompiledContract(p PrecompiledContract) StatefulPrecompiledContract {
	return &wrappedPrecompiledContract{p: p}
}

// Run implements the StatefulPrecompiledContract interface
func (w *wrappedPrecompiledContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// [caller.Address()] has already been verified
	// as having a sufficient balance before the
	// precompiled contract runs.
	evm.Context.Transfer(evm.StateDB, caller.Address(), addr, value)
	return RunPrecompiledContract(w.p, input, suppliedGas)
}

// nativeAssetBalance is a precompiled contract used to retrieve the native asset balance
type nativeAssetBalance struct {
	gasCost uint64
}

func PackNativeAssetBalanceInput(address common.Address, assetID common.Hash) []byte {
	input := make([]byte, 52)
	copy(input, address.Bytes())
	copy(input[20:], assetID.Bytes())
	return input
}

func UnpackNativeAssetBalanceInput(input []byte) (common.Address, common.Hash, error) {
	if len(input) != 52 {
		return common.Address{}, common.Hash{}, fmt.Errorf("native asset balance input had unexpcted length %d", len(input))
	}
	address := common.BytesToAddress(input[:20])
	assetID := common.Hash{}
	assetID.SetBytes(input[20:52])
	return address, assetID, nil
}

// Run implements StatefulPrecompiledContract
func (b *nativeAssetBalance) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes)
	if suppliedGas < b.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - b.gasCost

	if value.Sign() != 0 {
		return nil, remainingGas, ErrExecutionReverted
	}

	address, assetID, err := UnpackNativeAssetBalanceInput(input)
	if err != nil {
		return nil, remainingGas, ErrExecutionReverted
	}

	res, overflow := uint256.FromBig(evm.StateDB.GetBalanceMultiCoin(address, assetID))
	if overflow {
		return nil, remainingGas, ErrExecutionReverted
	}
	return common.LeftPadBytes(res.Bytes(), 32), remainingGas, nil
}

// nativeAssetCall atomically transfers a native asset to a recipient address as well as calling that
// address
type nativeAssetCall struct {
	gasCost uint64
}

func PackNativeAssetCallInput(address common.Address, assetID common.Hash, assetAmount *big.Int, callData []byte) []byte {
	input := make([]byte, 84+len(callData))
	copy(input[0:20], address.Bytes())
	copy(input[20:52], assetID.Bytes())
	assetAmount.FillBytes(input[52:84])
	copy(input[84:], callData)
	return input
}

func UnpackNativeAssetCallInput(input []byte) (common.Address, *common.Hash, *big.Int, []byte, error) {
	if len(input) < 84 {
		return common.Address{}, nil, nil, nil, fmt.Errorf("native asset call input had unexpcted length %d", len(input))
	}
	to := common.BytesToAddress(input[:20])
	assetID := new(common.Hash)
	assetID.SetBytes(input[20:52])
	assetAmount := new(big.Int).SetBytes(input[52:84])
	callData := input[84:]
	return to, assetID, assetAmount, callData, nil
}

// Run implements StatefulPrecompiledContract
func (c *nativeAssetCall) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes, assetAmount 32 bytes, callData variable length bytes)
	if suppliedGas < c.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - c.gasCost

	if readOnly {
		return nil, remainingGas, ErrExecutionReverted
	}

	to, assetID, assetAmount, callData, err := UnpackNativeAssetCallInput(input)
	if err != nil {
		return nil, remainingGas, ErrExecutionReverted
	}

	if assetAmount.Sign() != 0 && !evm.Context.CanTransferMC(evm.StateDB, caller.Address(), to, assetID, assetAmount) {
		return nil, remainingGas, ErrInsufficientBalance
	}

	snapshot := evm.StateDB.Snapshot()

	if !evm.StateDB.Exist(to) {
		if remainingGas < params.CallNewAccountGas {
			return nil, 0, ErrOutOfGas
		}
		remainingGas -= params.CallNewAccountGas
		evm.StateDB.CreateAccount(to)
	}

	// Send [value] to [to] address
	evm.Context.Transfer(evm.StateDB, caller.Address(), to, value)
	evm.Context.TransferMultiCoin(evm.StateDB, caller.Address(), to, assetID, assetAmount)
	ret, remainingGas, err = evm.Call(caller, to, callData, remainingGas, big.NewInt(0))

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		// If there is an error, it has already been logged,
		// so we convert it to [ErrExecutionReverted] here
		// to prevent consuming all of the gas. This is the
		// equivalent of adding a conditional revert.
		err = ErrExecutionReverted
		// if err != ErrExecutionReverted {
		// 	remainingGas = 0
		// }
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

var (
	transferSignature   = EncodeSignatureHash("transfer(address,uint256,uint256,uint256)")
	getBalanceSignature = EncodeSignatureHash("getBalance(uint256)")
)

func EncodeSignatureHash(functionSignature string) []byte {
	hash := crypto.Keccak256([]byte(functionSignature))
	return hash[:4]
}

// genesisContract mimics the genesis contract Multicoin.sol in the original
// coreth release. It does this by mapping function identifiers to their new
// implementations via the native asset precompiled contracts.
type genesisContract struct{}

func (g *genesisContract) Run(evm *EVM, caller ContractRef, addr common.Address, value *big.Int, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if len(input) < 4 {
		return nil, suppliedGas, ErrExecutionReverted
	}

	if value.Sign() != 0 {
		return nil, suppliedGas, ErrExecutionReverted
	}

	functionSignature := input[:4]
	switch {
	case bytes.Equal(functionSignature, transferSignature):
		if len(input) != 132 {
			return nil, suppliedGas, ErrExecutionReverted
		}

		// address / value1 / assetID / assetAmount
		args := input[4:]
		// Require that the left padded bytes are all zeroes
		if !bytes.Equal(args[0:12], make([]byte, 12)) {
			return nil, suppliedGas, ErrExecutionReverted
		}
		addressBytes := args[12:32]
		callAssetArgs := make([]byte, 84)
		copy(callAssetArgs[:20], addressBytes)
		newValue := new(big.Int).SetBytes(args[32:64])
		copy(callAssetArgs[20:52], args[64:96])  // Copy the assetID bytes
		copy(callAssetArgs[52:84], args[96:128]) // Copy the assetAmount to be transferred
		return evm.Call(caller, nativeAssetCallAddr, callAssetArgs, suppliedGas, newValue)
	case bytes.Equal(functionSignature, getBalanceSignature):
		if len(input) != 36 {
			return nil, suppliedGas, ErrExecutionReverted
		}
		balanceArgs := make([]byte, 52)
		copy(balanceArgs[:20], caller.Address().Bytes())
		copy(balanceArgs[20:52], input[4:36])
		return evm.Call(caller, nativeAssetBalanceAddr, balanceArgs, suppliedGas, value)
	default:
		return nil, suppliedGas, ErrExecutionReverted
	}
}
