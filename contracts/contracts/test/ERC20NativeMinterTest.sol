//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../ERC20NativeMinter.sol";
import "../interfaces/INativeMinter.sol";
import "./AllowListTest.sol";

// TODO:
// this contract adds another (unwanted) layer of indirection
// but it's the easiest way to match the previous HardHat testing functionality.
// Once we completely migrate to DS-test, we can simplify this set of tests.
contract Minter {
  ERC20NativeMinter token;

  constructor(address tokenAddress) {
    token = ERC20NativeMinter(tokenAddress);
  }

  function mintdraw(uint amount) external {
    token.mintdraw(amount);
  }

  function deposit(uint value) external {
    token.deposit{value: value}();
  }
}

contract ERC20NativeMinterTest is AllowListTest {
  INativeMinter nativeMinter = INativeMinter(MINTER_ADDRESS);

  function setUp() public {
    // noop
  }

  function step_mintdrawFailure() public {
    ERC20NativeMinter token = new ERC20NativeMinter(1000);
    address tokenAddress = address(token);

    assertRole(nativeMinter.readAllowList(tokenAddress), AllowList.Role.None);

    try token.mintdraw(100) {
      assertTrue(false, "mintdraw should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected
  }

  function step_addMinter() public {
    ERC20NativeMinter token = new ERC20NativeMinter(1000);
    address tokenAddress = address(token);

    assertRole(nativeMinter.readAllowList(tokenAddress), AllowList.Role.None);

    nativeMinter.setEnabled(tokenAddress);

    assertRole(nativeMinter.readAllowList(tokenAddress), AllowList.Role.Enabled);
  }

  function step_adminMintdraw() public {
    ERC20NativeMinter token = new ERC20NativeMinter(1000);
    address tokenAddress = address(token);

    address testAddress = address(this);

    nativeMinter.setEnabled(tokenAddress);

    uint initialTokenBalance = token.balanceOf(testAddress);
    uint initialNativeBalance = testAddress.balance;

    uint amount = 100;

    token.mintdraw(amount);

    assertEq(token.balanceOf(testAddress), initialTokenBalance - amount);
    assertEq(testAddress.balance, initialNativeBalance + amount);
  }

  function step_minterMintdrawFailure() public {
    ERC20NativeMinter token = new ERC20NativeMinter(1000);
    address tokenAddress = address(token);

    Minter minter = new Minter(tokenAddress);
    address minterAddress = address(minter);

    nativeMinter.setEnabled(tokenAddress);

    uint initialTokenBalance = token.balanceOf(minterAddress);
    uint initialNativeBalance = minterAddress.balance;

    assertEq(initialTokenBalance, 0);

    try minter.mintdraw(100) {
      assertTrue(false, "mintdraw should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    assertEq(token.balanceOf(minterAddress), initialTokenBalance);
    assertEq(minterAddress.balance, initialNativeBalance);
  }

  function step_minterDeposit() public {
    ERC20NativeMinter token = new ERC20NativeMinter(1000);
    address tokenAddress = address(token);

    Minter minter = new Minter(tokenAddress);
    address minterAddress = address(minter);

    nativeMinter.setEnabled(tokenAddress);

    uint amount = 100;

    nativeMinter.mintNativeCoin(minterAddress, amount);

    uint initialTokenBalance = token.balanceOf(minterAddress);
    uint initialNativeBalance = minterAddress.balance;

    minter.deposit(amount);

    assertEq(token.balanceOf(minterAddress), initialTokenBalance + amount);
    assertEq(minterAddress.balance, initialNativeBalance - amount);
  }

  function step_mintdraw() public {
    ERC20NativeMinter token = new ERC20NativeMinter(1000);
    address tokenAddress = address(token);

    Minter minter = new Minter(tokenAddress);
    address minterAddress = address(minter);

    nativeMinter.setEnabled(tokenAddress);

    uint amount = 100;

    uint initialNativeBalance = minterAddress.balance;
    assertEq(initialNativeBalance, 0);

    token.mint(minterAddress, amount);

    uint initialTokenBalance = token.balanceOf(minterAddress);
    assertEq(initialTokenBalance, amount);

    minter.mintdraw(amount);

    assertEq(token.balanceOf(minterAddress), 0);
    assertEq(minterAddress.balance, amount);
  }
}
