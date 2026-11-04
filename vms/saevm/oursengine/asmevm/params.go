package asmevm

// Cancun gas constants. Names follow the yellow paper / EIP terminology.
const (
	GasQuickStep   = 2
	GasFastestStep = 3
	GasFastStep    = 5
	GasMidStep     = 8
	GasSlowStep    = 10
	GasExtStep     = 20

	Keccak256Gas     = 30
	Keccak256WordGas = 6
	CopyGas          = 3
	LogGas           = 375
	LogTopicGas      = 375
	LogDataGas       = 8
	ExpByteGas       = 50 // EIP-158
	CreateGas        = 32000
	Create2Gas       = 32000 // base; hashing charged on top
	CreateDataGas    = 200   // per code byte deployed
	MemoryGas        = 3
	JumpdestGas      = 1

	CallValueTransferGas = 9000
	CallNewAccountGas    = 25000
	CallStipend          = 2300

	// EIP-2929
	ColdAccountAccessCost = 2600
	ColdSloadCost         = 2100
	WarmStorageReadCost   = 100

	// EIP-2200 / EIP-3529 SSTORE
	SstoreSetGas       = 20000
	SstoreResetGas     = 5000 // minus cold sload cost at charge time
	SstoreClearsRefund = 4800 // EIP-3529: SstoreResetGas - ColdSloadCost + AccessListStorageKeyCost(1900)... use EIP-3529 value

	// EIP-3860
	InitCodeWordGas = 2
	MaxInitCodeSize = 49152
	MaxCodeSize     = 24576

	TxGas                     = 21000
	TxGasContractCreation     = 53000
	TxDataZeroGas             = 4
	TxDataNonZeroGas          = 16 // EIP-2028
	TxAccessListAddressGas    = 2400
	TxAccessListStorageKeyGas = 1900

	RefundQuotientEIP3529 = 5

	BlobHashGas = 3

	CallGasCapDivisor = 64 // EIP-150 63/64 rule

	MaxCallDepth = 1024
)

const SelfdestructGas = 5000
