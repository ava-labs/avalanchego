// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TrieStressTest {
  bytes32[] private data;

  function writeValues(uint value) public {
    bytes32 dataToPush = bytes32(uint256(uint160(msg.sender)) << 96);
    for (uint i = 0; i < value; i++) {
      data.push(dataToPush);
    }
  }

  function getLength() public view returns (uint) {
    return data.length;
  }

  function getData(uint index) public view returns (bytes32) {
    require(index < data.length, "Index out of bound");
    return data[index];
  }
}
