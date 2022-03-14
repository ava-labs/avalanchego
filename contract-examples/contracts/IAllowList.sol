//SPDX-License-Identifier: MIT
pragma solidity >=0.6.2;

interface IAllowList {
  // Set [addr] to have the admin role over the minter list
  function setAdmin(address addr) external;

  // Set [addr] to be enabled on the minter list
  function setEnabled(address addr) external;

  // Set [addr] to have no role over the minter list
  function setNone(address addr) external;

  // Read the status of [addr]
  function readAllowList(address addr) external view returns (uint256);
}
