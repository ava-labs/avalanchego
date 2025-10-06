import "@nomicfoundation/hardhat-toolbox";
import "./tasks"

// HardHat users must populate these environment variables in order to connect to their subnet-evm instance
// Since the blockchainID is not known in advance, there's no good default to use and we use the C-Chain here.
var local_rpc_uri = process.env.RPC_URI || "http://127.0.0.1:9650/ext/bc/C/rpc"
var local_chain_id = parseInt(process.env.CHAIN_ID, 10) || 99999

export default {
  solidity: {
    compilers: [
      {
        version: "0.8.24",
        settings: {
          evmVersion: "shanghai",
        },
      },
    ]
  },
  networks: {
    local: {
      //"http://{ip}:{port}/ext/bc/{chainID}/rpc
      // expected to be populated by the environment variables above
      url: local_rpc_uri,
      chainId: local_chain_id,
      accounts: [
        "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
        "0x7b4198529994b0dc604278c99d153cfd069d594753d471171a1d102a10438e07",
        "0x15614556be13730e9e8d6eacc1603143e7b96987429df8726384c2ec4502ef6e",
        "0x31b571bf6894a248831ff937bb49f7754509fe93bbd2517c9c73c4144c0e97dc",
        "0x6934bef917e01692b789da754a0eae31a8536eb465e7bff752ea291dad88c675",
        "0xe700bdbdbc279b808b1ec45f8c2370e4616d3a02c336e68d85d4668e08f53cff",
        "0xbbc2865b76ba28016bc2255c7504d000e046ae01934b04c694592a6276988630",
        "0xcdbfd34f687ced8c6968854f8a99ae47712c4f4183b78dcc4a903d1bfe8cbf60",
        "0x86f78c5416151fe3546dece84fda4b4b1e36089f2dbc48496faf3a950f16157c",
        "0x750839e9dbbd2a0910efe40f50b2f3b2f2f59f5580bb4b83bd8c1201cf9a010a"
      ],
      pollingInterval: "1s"
    },
  },
  mocha: {
    timeout: 30000
  },
  gasReporter: {
    enabled: (process.env.REPORT_GAS) ? true : false
  }
}
