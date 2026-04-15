// SPDX-License-Identifier: Apache-2.0
pragma solidity ^0.8.17;

import "remapped-fake-remote-repo/Echo.sol";

contract HelloWorld {
    Echoer private immutable echoer;

    constructor() {
        echoer = new Echoer();
    }

    function hello() external view returns (string memory) {
        return echoer.echo("hello world");
    }
}
