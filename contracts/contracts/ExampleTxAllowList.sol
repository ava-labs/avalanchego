//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./AllowList.sol";
import "./interfaces/IAllowList.sol";

// Precompiled Allow List Contract Address
address constant TX_ALLOW_LIST = 0x0200000000000000000000000000000000000002;
address constant OTHER_ADDRESS = 0x0Fa8EA536Be85F32724D57A37758761B86416123;

// ExampleTxAllowList shows how TxAllowList precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleTxAllowList is AllowList {
  constructor() AllowList(TX_ALLOW_LIST) {}

  function deployContract() public {
    new Example();
  }
}

contract Example {}
