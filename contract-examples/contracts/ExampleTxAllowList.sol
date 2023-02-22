//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./AllowList.sol";

// ExampleTxAllowList shows how TxAllowList precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleTxAllowList is AllowList {
  // Precompiled Allow List Contract Address
  address constant TX_ALLOW_LIST = 0x0200000000000000000000000000000000000002;

  constructor() AllowList(TX_ALLOW_LIST) {}
}
