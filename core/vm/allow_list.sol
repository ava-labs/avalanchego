// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity >=0.8.0;

library AllowList {
    address constant allowListAddr = 0x0200000000000000000000000000000000000000;

    function remove(address addr) public {
        _invokeAllowList(addr, 0);
    }
    
    function setDeployer(address addr) public {
        _invokeAllowList(addr, 1);
    }

    function setAdmin(address addr) public {
        _invokeAllowList(addr, 2);
    }
    
    function _invokeAllowList(address addr, uint256 role) internal {
        (bool success, ) = allowListAddr.call(abi.encodePacked(addr, role));
        require(success);
    }
}

