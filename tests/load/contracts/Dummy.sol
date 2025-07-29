// SPDX-License-Identifier: MIT
pragma solidity ^0.8.21;

/// @notice Dummy is a contract used only for simulating the load
/// of contract creation operations in the EVM. It has state variables,
/// a constructor and simple functions to have a real-like deployed size.
contract Dummy {
    uint256 public value;
    address public owner;
    mapping(uint256 => uint256) public data;

    event ValueUpdated(uint256 newValue);
    event DataWritten(uint256 key, uint256 value);

    constructor() {
        value = 42;
        owner = msg.sender;
    }

    function updateValue(uint256 newValue) external {
        value = newValue;
    }

    function writeData(uint256 key, uint256 val) external {
        data[key] = val;
        emit DataWritten(key, val);
    }

    function readData(uint256 key) external view returns (uint256) {
        return data[key];
    }
}
