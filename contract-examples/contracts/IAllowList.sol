//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IAllowList {
  // Set [addr] to have the admin role over the precompile contract.
  function setAdmin(address addr) external;

  // Set [addr] to be enabled on the precompile contract.
  function setEnabled(address addr) external;

  // Set [addr] to have no role for the precompile contract.
  function setNone(address addr) external;

  // Read the status of [addr].
  function readAllowList(address addr) external view returns (uint256 role);
}
