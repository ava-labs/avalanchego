//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/allowlist/IAllowList.sol";

contract AllowListTest {
    IAllowList private allowList;

    uint256 constant STATUS_NONE = 0;
    uint256 constant STATUS_ENABLED = 1;
    uint256 constant STATUS_ADMIN = 2;
    uint256 constant STATUS_MANAGER = 3;

    constructor(address precompileAddr) {
        allowList = IAllowList(precompileAddr);
    }

    function setAdmin(address addr) external {
        allowList.setAdmin(addr);
    }

    function setEnabled(address addr) external {
        allowList.setEnabled(addr);
    }

    function setManager(address addr) external {
        allowList.setManager(addr);
    }

    function setNone(address addr) external {
        allowList.setNone(addr);
    }

    function readAllowList(address addr) external view returns (uint256) {
        return allowList.readAllowList(addr);
    }

    // Helper functions used by tests
    function isAdmin(address addr) public view returns (bool) {
        return allowList.readAllowList(addr) == STATUS_ADMIN;
    }

    function isManager(address addr) public view returns (bool) {
        return allowList.readAllowList(addr) == STATUS_MANAGER;
    }

    function isEnabled(address addr) public view returns (bool) {
        // Returns true if address has any role (not NONE)
        return allowList.readAllowList(addr) != STATUS_NONE;
    }

    function revoke(address addr) public {
        require(msg.sender != addr, "cannot revoke own role");
        allowList.setNone(addr);
    }

    // Used by deployerallowlist tests to verify contract deployment permissions
    function deployContract() public {
        new Example();
    }
}

// This is an empty contract that can be used to test contract deployment
contract Example {}
