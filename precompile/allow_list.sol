// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity >=0.8.0;

library AllowList {
    address constant modifyAllowListAddr = 0x0200000000000000000000000000000000000000;
    address constant readAllowListAddr = 0x0200000000000000000000000000000000000000;

    function remove(address addr) public {
        _invokeAllowList(addr, 0);
    }
    
    function setDeployer(address addr) public {
        _invokeAllowList(addr, 1);
    }

    function canDeploy(address addr) public view returns (bool) {
        uint256 role = _readAllowListRole(addr);
        return role == 2 || role == 1;
    }

    function setAdmin(address addr) public {
        _invokeAllowList(addr, 2);
    }

    function isAdmin(address addr) public view returns (bool) {
        uint256 role = _readAllowListRole(addr);
        return role == 2;
    }
    
    function _invokeAllowList(address addr, uint256 role) internal {
        (bool success, ) = modifyAllowListAddr.call(abi.encodePacked(addr, role));
        require(success);
    }

    function _readAllowListRole(address addr) internal view returns (uint256) {
        (bool success, bytes memory data) = readAllowListAddr.staticcall(abi.encodePacked(addr));
        require(success);
        return abi.decode(data, (uint256));
    }
}
