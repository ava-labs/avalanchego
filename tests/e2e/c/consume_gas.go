// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

const (
	// consumeGasCompiledContract is the compiled bytecode of the contract
	// defined in consume_gas.sol.
	consumeGasCompiledContract = "6080604052348015600f57600080fd5b5060788061001e6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c8063c040622614602d575b600080fd5b60336035565b005b5b6001156040576036565b56fea264697066735822122070cfeeb0992270b4ff725a1264654534853d25ea6bb28c85d986beccfdbc997164736f6c63430008060033"
	// consumeGasABIJson is the ABI of the contract defined in consume_gas.sol.
	consumeGasABIJson = `[{"inputs":[],"name":"run","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
	// consumeGasFunction is the name of the function to call to consume gas.
	consumeGasFunction = "run"
)
