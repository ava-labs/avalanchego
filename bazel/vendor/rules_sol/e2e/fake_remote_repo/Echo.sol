// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.17;

contract Echoer {
    function echo(string memory payload) external pure returns (string memory) {
        return payload;
    }
}
