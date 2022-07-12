//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;
import "./IAllowList.sol";

interface INativeMinter is IAllowList {
  // Mint [amount] number of native coins and send to [addr]
  function mintNativeCoin(address addr, uint256 amount) external;
}
