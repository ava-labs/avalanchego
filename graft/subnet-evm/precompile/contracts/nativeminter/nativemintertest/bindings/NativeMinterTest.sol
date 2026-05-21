//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "precompile/contracts/nativeminter/INativeMinter.sol";

contract NativeMinterTest {
    INativeMinter private nativeMinter;

    constructor(address nativeMinterPrecompile) {
        nativeMinter = INativeMinter(nativeMinterPrecompile);
    }

    // Calls the mintNativeCoin function on the precompile
    function mintNativeCoin(address addr, uint256 amount) external {
        nativeMinter.mintNativeCoin(addr, amount);
    }

    // Allows this contract to receive native coins
    receive() external payable {}
}
