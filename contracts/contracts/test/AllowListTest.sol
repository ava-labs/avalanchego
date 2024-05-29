//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../AllowList.sol";
import "ds-test/src/test.sol";

contract AllowListTest is DSTest {
  function assertRole(uint result, AllowList.Role role) internal {
    assertEq(result, uint(role));
  }
}
