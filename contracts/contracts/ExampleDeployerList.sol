//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./interfaces/IAllowList.sol";
import "./AllowList.sol";

address constant DEPLOYER_LIST = 0x0200000000000000000000000000000000000000;
address constant OTHER_ADDRESS = 0x0Fa8EA536Be85F32724D57A37758761B86416123;

// ExampleDeployerList shows how ContractDeployerAllowList precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleDeployerList is AllowList {
  // Precompiled Allow List Contract Address
  constructor() AllowList(DEPLOYER_LIST) {}

  function deployContract() public {
    new Example();
  }
}

// This is an empty contract that can be used to test contract deployment
contract Example {

}
