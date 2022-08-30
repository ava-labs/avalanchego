# Subnets

The Avalanche network consists of the Primary Network and a collection of
sub-networks (subnets).

## Subnet Creation

Subnets are created by issuing a *CreateSubnetTx*. After a *CreateSubnetTx* is
accepted, a new subnet will exist with the *SubnetID* equal to the *TxID* of the
*CreateSubnetTx*. The *CreateSubnetTx* creates a permissioned subnet. The
*Owner* field in *CreateSubnetTx* specifies who can modify the state of the
subnet.

## Permissioned Subnets

A permissioned subnet can be modified by a few different transactions.

- CreateChainTx
  - Creates a new chain that will be validated by all validators of the subnet.
- AddSubnetValidatorTx
  - Adds a new validator to the subnet with the specified *StartTime*,
    *EndTime*, and *Weight*.
- RemoveSubnetValidatorTx
  - Removes a validator from the subnet.
- TransformSubnetTx
  - Converts the permissioned subnet into a permissionless subnet.
  - Specifies all of the staking parameters.
    - AVAX is not allowed to be used as a staking token. In general, it is not
      advisable to have multiple subnets using the same staking token.
  - After becoming a permissionless subnet, previously added permissioned
    validators will remain to finish their staking period.
  - No more chains will be able to be added to the subnet.

### Permissionless Subnets

Currently, nothing can be performed on a permissionless subnet.
