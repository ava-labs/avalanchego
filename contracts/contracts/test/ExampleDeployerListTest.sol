//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../ExampleDeployerList.sol";
import "../interfaces/IAllowList.sol";
import "../AllowList.sol";
import "./AllowListTest.sol";

// ExampleDeployerListTest defines transactions that are used to test
// the DeployerAllowList precompile by instantiating and calling the
// ExampleDeployerList and making assertions.
// The transactions are put together as steps of a complete test in contract_deployer_allow_list.ts.
// TODO: a bunch of these tests have repeated code that should be combined
contract ExampleDeployerListTest is AllowListTest {
  address constant OTHER_ADDRESS = 0x0Fa8EA536Be85F32724D57A37758761B86416123;

  IAllowList allowList = IAllowList(DEPLOYER_LIST);
  ExampleDeployerList private example;

  function setUp() public {
    example = new ExampleDeployerList();
    allowList.setNone(OTHER_ADDRESS);
  }

  function step_verifySenderIsAdmin() public {
    assertRole(allowList.readAllowList(msg.sender), AllowList.Role.Admin);
  }

  function step_newAddressHasNoRole() public {
    address exampleAddress = address(example);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);
  }

  function step_noRoleIsNotAdmin() public {
    address exampleAddress = address(example);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);
    assertTrue(!example.isAdmin(exampleAddress));
  }

  function step_ownerIsAdmin() public {
    address exampleAddress = address(example);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);
    assertTrue(example.isAdmin(address(this)));
  }

  function step_noRoleCannotDeploy() public {
    assertRole(allowList.readAllowList(tx.origin), AllowList.Role.None);

    try example.deployContract() {
      assertTrue(false, "deployContract should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected
  }

  function step_adminAddContractAsAdmin() public {
    address exampleAddress = address(example);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);

    allowList.setAdmin(exampleAddress);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.Admin);

    assertTrue(example.isAdmin(exampleAddress));
  }

  function step_addDeployerThroughContract() public {
    ExampleDeployerList other = new ExampleDeployerList();
    address exampleAddress = address(example);
    address otherAddress = address(other);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);

    allowList.setAdmin(exampleAddress);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.Admin);

    example.setEnabled(otherAddress);

    assertTrue(example.isEnabled(otherAddress));
  }

  function step_deployerCanDeploy() public {
    ExampleDeployerList deployer = new ExampleDeployerList();
    address exampleAddress = address(example);
    address deployerAddress = address(deployer);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);

    allowList.setAdmin(exampleAddress);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.Admin);

    example.setEnabled(deployerAddress);

    assertTrue(example.isEnabled(deployerAddress));

    deployer.deployContract();
  }

  function step_adminCanRevokeDeployer() public {
    ExampleDeployerList deployer = new ExampleDeployerList();
    address exampleAddress = address(example);
    address deployerAddress = address(deployer);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);

    allowList.setAdmin(exampleAddress);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.Admin);

    example.setEnabled(deployerAddress);

    assertTrue(example.isEnabled(deployerAddress));

    example.revoke(deployerAddress);

    assertRole(allowList.readAllowList(deployerAddress), AllowList.Role.None);
  }
}
