// SPDX-License-Identifier: MIT

pragma solidity = 0.8.6;

contract ConsumeGas {

  bytes hashVar = bytes("This is the hashing text for the test");

  function hashIt() public {
    for (uint i=0; i<3700; i++) {
      ripemd160(hashVar);
    }
  }

}
