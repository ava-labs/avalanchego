//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./IAllowList.sol";
import "./AllowList.sol";

// ExampleDeployerList shows how ContractDeployerAllowList precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleDeployerList is AllowList {
  // Precompiled Allow List Contract Address
  address constant DEPLOYER_LIST = 0x0200000000000000000000000000000000000000;

  constructor() AllowList(DEPLOYER_LIST) {}
}
