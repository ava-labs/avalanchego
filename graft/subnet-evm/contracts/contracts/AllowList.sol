//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./interfaces/IAllowList.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

// AllowList is a base contract to use AllowList precompile capabilities.
contract AllowList is Ownable {
  // Precompiled Allow List Contract Address
  IAllowList private allowList;

  uint256 constant STATUS_NONE = 0;
  uint256 constant STATUS_ENABLED = 1;
  uint256 constant STATUS_ADMIN = 2;
  uint256 constant STATUS_MANAGER = 3;

  enum Role {
    None,
    Enabled,
    Admin,
    Manager
  }

  constructor(address precompileAddr) Ownable(msg.sender) {
    allowList = IAllowList(precompileAddr);
  }

  modifier onlyEnabled() {
    require(isEnabled(msg.sender), "not enabled");
    _;
  }

  function isAdmin(address addr) public view returns (bool) {
    uint256 result = allowList.readAllowList(addr);
    return result == STATUS_ADMIN;
  }

  function isManager(address addr) public view returns (bool) {
    uint256 result = allowList.readAllowList(addr);
    return result == STATUS_MANAGER;
  }

  function isEnabled(address addr) public view returns (bool) {
    uint256 result = allowList.readAllowList(addr);
    // if address is ENABLED or ADMIN or MANAGER it can deploy
    // in other words, if it's not NONE it can deploy.
    return result != STATUS_NONE;
  }

  function setAdmin(address addr) public virtual onlyOwner {
    _setAdmin(addr);
  }

  function _setAdmin(address addr) private {
    allowList.setAdmin(addr);
  }

  function setManager(address addr) public virtual onlyOwner {
    _setManager(addr);
  }

  function _setManager(address addr) private {
    allowList.setManager(addr);
  }

  function setEnabled(address addr) public virtual onlyOwner {
    _setEnabled(addr);
  }

  function _setEnabled(address addr) private {
    allowList.setEnabled(addr);
  }

  function revoke(address addr) public virtual onlyOwner {
    _revoke(addr);
  }

  function _revoke(address addr) private {
    require(msg.sender != addr, "cannot revoke own role");
    allowList.setNone(addr);
  }
}
