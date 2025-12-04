//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./IAllowList.sol";
import "./AllowList.sol";

contract AllowListTest is AllowList {
    // Precompiled Allow List Contract Address
    constructor(address precompileAddr) AllowList(precompileAddr) {}

    function deployContract() public {
        new Example();
    }
}

// This is an empty contract that can be used to test contract deployment
contract Example {}
