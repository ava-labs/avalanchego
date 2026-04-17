// SPDX-License-Identifier: BSD-3-Clause
pragma solidity ^0.8.0;

contract HelloWorld {
    event Hello(string);

    function greet(string memory who) external {
        emit Hello(who);
    }
}
