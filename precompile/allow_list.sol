// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity >=0.8.0;

interface AllowListInterface {
    // Set [addr] to have the admin role over the allow list
    function setAdmin(address addr) external;

    // Set [addr] to be enabled on the allow list
    function setEnabled(address addr) external;

    // Set [addr] to have no role over the allow list
    function setNone(address addr) external;

    // Read the status of [addr]
    function readAllowList(address addr) external view returns (uint256);
}
