# Release Notes

## Pending (v1.14.1)

This version is backwards compatible to [v1.14.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.14.0). It is optional, but encouraged.

### APIs

- ...

### Config

- Added `--system-tracker-disk-required-available-space` and `--system-tracker-disk-warning-available-space-percentage` options.
- Deprecate `--system-tracker-disk-required-available-space` and `--system-tracker-disk-warning-threshold-available-space` options.

### Fixes

- Update go version to 1.24.11

### EVM

- Removes `avax.version` API
- Removes `customethclient` package in favor of `ethclient` package and temporary type registrations (`WithTempRegisteredLibEVMExtras`)
  - Also removes blockHook extension in `ethclient` package.
- Enables Firewood to run with pruning disabled.
  - This change modifies the filepath of Firewood and any nodes using Firewood will need to resync.

## [v1.14.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.14.0)

This release schedules the activation of the following Avalanche Community Proposals (ACPs):
- [ACP-181](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/181-p-chain-epoched-views/README.md) P-Chain Epoched Views
- [ACP-204](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/204-precompile-secp256r1/README.md) Precompile for secp256r1 Curve Support
- [ACP-226](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md) Dynamic Minimum Block Times

The ACPs in this upgrade go into effect at 11 AM ET (4 PM UTC) on Wednesday, November 19th, 2025 on Mainnet.

**All Mainnet nodes must upgrade before 11 AM ET, November 19th 2025.**

The plugin version is updated to `44`; all plugins must update to be compatible.

### LibEVM Hook Registration

This release includes changes for how EVM modifications are registered with libevm. For consumers of avalanchego as a library, manual registration of libevm callbacks is now required.

### APIs

- Added support for specifying multiple `Avalanche-Api-Route` headers for more complex routing.
- Added proposervm gRPC, connectrpc, and jsonrpc APIs for `GetProposedHeight` and `GetCurrentEpoch`.
  - The gRPC and connectrpc APIs are routed by adding a second `Avalanche-Api-Route` header with the value `proposervm`.
  - The jsonrpc APIs are added to all chains with the base endpoint `/proposervm`.
- Added platformvm `platform.GetAllValidatorsAt` API.

### Configs

- Changed default `proposerMinBlockDelay` for L1s (other than Primary Network) to 0.

### Fixes

- Improved bootstrapping ETA predictions.

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.13.5...v1.14.0

## [v1.13.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.5)

This version is backwards compatible to [v1.13.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0). It is optional, but encouraged.

The plugin version is unchanged at `43` and is compatible with version `v1.13.4`.

### Configs

- Replaced `pull-gossip-throttling-limit` with `pull-gossip-requests-per-validator` in the X-Chain and P-Chain configs

### Fixes

- Fixed Firewood performance regression
- Fixed duplicate C-Chain eth gossip registration
- Fixed various C-Chain atomic mempool edge cases

### What's Changed

- Separate re-execution job params for PR from schedule by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4151
- chore: fix a typo in gossip,go by @Galoretka in https://github.com/ava-labs/avalanchego/pull/4154
- Add @joshua-kim as CODEOWNER to testing-related packages by @JuanLeon2 in https://github.com/ava-labs/avalanchego/pull/4118
- refactor(load): remove context from test interface by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4157
- Move C-Chain benchmark to custom action and add ARC + GH runner triggers by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4165
- Use EmptyVoteMetadata in Simplex Proto Messages by @samliok in https://github.com/ava-labs/avalanchego/pull/4174
- feat(load): add token test by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4171
- Fix typo in comment - PChainHeight context by @yacovm in https://github.com/ava-labs/avalanchego/pull/4176
- chore: fix function name by @yinwenyu6 in https://github.com/ava-labs/avalanchego/pull/4178
- chore: fix typo by @kks-code in https://github.com/ava-labs/avalanchego/pull/4179
- Migrate predicate package from evm repos by @JonathanOppenheimer in https://github.com/ava-labs/avalanchego/pull/4147
- feat: add eviction callback in LRU cache by @DracoLi in https://github.com/ava-labs/avalanchego/pull/4088
- `config/config.md:` Added Env Variable representation of flags + improved UI design by @navillanueva in https://github.com/ava-labs/avalanchego/pull/4110
- Storage Component For Simplex by @samliok in https://github.com/ava-labs/avalanchego/pull/4122
- Block Database by @DracoLi in https://github.com/ava-labs/avalanchego/pull/4027
- Change cache path to tmp included in gitignore by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4183
- Add optional step to archive post-reexecution state to S3 by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4172
- Rename height field to numBlocks by @samliok in https://github.com/ava-labs/avalanchego/pull/4184
- refactor: replace []byte(fmt.Sprintf) with fmt.Appendf by @queryfast in https://github.com/ava-labs/avalanchego/pull/4161
- Make Draco the codeowner of the blockdb by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4187
- Add redundant import alias linting by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4191
- Add config option for AWS S3 read only credential duration by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4192
- fix: blockdb file eviction race issue by @DracoLi in https://github.com/ava-labs/avalanchego/pull/4186
- Count throttled requests as hits by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4199
- Rename Engine Types by @samliok in https://github.com/ava-labs/avalanchego/pull/4193
- Add s5cmd progress bar by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4204
- Update block + validator + pgo checkpoints to 2025-08-23 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4205
- Remove buf lint action by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4189
- Add ability to create zstd compressor with compression level by @DracoLi in https://github.com/ava-labs/avalanchego/pull/4203
- Dynamically update mempool gossip request rate limit by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4162
- Add support for passing config and predefined configs to VM re-execution tests by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4180

### New Contributors

- @Galoretka made their first contribution in https://github.com/ava-labs/avalanchego/pull/4154
- @JuanLeon2 made their first contribution in https://github.com/ava-labs/avalanchego/pull/4118
- @yinwenyu6 made their first contribution in https://github.com/ava-labs/avalanchego/pull/4178
- @kks-code made their first contribution in https://github.com/ava-labs/avalanchego/pull/4179
- @navillanueva made their first contribution in https://github.com/ava-labs/avalanchego/pull/4110
- @queryfast made their first contribution in https://github.com/ava-labs/avalanchego/pull/4161

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.13.4...v1.13.5

## [v1.13.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.4)

This version is backwards compatible to [v1.13.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0). It is optional, but encouraged.

The plugin version is updated to `43` all plugins must update to be compatible.

### Configs

- Added `staking-rpc-signer-endpoint` flag.

### What's Changed

- Bump golang.org/x/oauth2 from 0.21.0 to 0.27.0 by @dependabot[bot] in https://github.com/ava-labs/avalanchego/pull/4100
- [tmpnet] Enable externally accessible URIs for kube-hosted nodes by @maru-ava in https://github.com/ava-labs/avalanchego/pull/4016
- [antithesis] Enable reuse of banff e2e test for antithesis testing by @marun in https://github.com/ava-labs/avalanchego/pull/3554
- feat(load2): add contract tests by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4071
- Add test to re-execute specified range of mainnet C-Chain blocks by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4019
- [tmpnet] Enable installation of chaos mesh to local kind cluster by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3674
- Update release notes and bump version to `v1.13.4` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4113
- Update reexecute c-chain range cronjob to run daily by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4115
- Remove Stale References of the toEngine Channel by @samliok in https://github.com/ava-labs/avalanchego/pull/4101
- Add step to push benchmark results to gh-pages by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4103
- ci: remove load 1.0 by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4106
- Simplex QuorumCertificate and BLS aggregator by @samliok in https://github.com/ava-labs/avalanchego/pull/4091
- Update codeowners of reexecution changes by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4116
- refactor: use maps.Copy for cleaner map handling by @jishudashu in https://github.com/ava-labs/avalanchego/pull/4119
- refactor: remove load 1.0  by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4112
- Enable Cubist Signer integration by @geoff-vball in https://github.com/ava-labs/avalanchego/pull/3965
- Split action benchmark comparison and push to gh-pages by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4130
- Remove external-data-json-path from benchmark push step by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4134
- With golangci-lint v2.2.2 using http.NewRequest is discouraged by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4136
- Add runner input to run c-chain reexecution benchmark on arbitrary target by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4121
- Remove flaky dial throttler tests by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4139
- Remove gitignore line that ignores the `database/dbtest` package by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4142
- chore: Update header year to 2025 by @JonathanOppenheimer in https://github.com/ava-labs/avalanchego/pull/4140
- feat(load): add trie stress test by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4137
- Parameterize values in transfer tests by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4144
- uplift: Add combined metrics package from evm repositories by @JonathanOppenheimer in https://github.com/ava-labs/avalanchego/pull/4135
- docs: load by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4132
- chore: fix minor typo in comment by @lechpzn in https://github.com/ava-labs/avalanchego/pull/4150
- fix metrics tests by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4146
- Update coreth to v0.15.3-rc.5 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4153

### New Contributors

- @jishudashu made their first contribution in https://github.com/ava-labs/avalanchego/pull/4119
- @lechpzn made their first contribution in https://github.com/ava-labs/avalanchego/pull/4150

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.13.3...v1.13.4

## [v1.13.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.3)

This version is backwards compatible to [v1.13.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0). It is optional, but encouraged.

The plugin version is updated to `42` all plugins must update to be compatible.

**This release removes the support for running on Windows. Any users running on Windows should utilize Windows Subsystem for Linux.**

### APIs

- Converted HTTP2 support to route by the `Avalanche-Api-Route` header.
- Added `avalanche_snowman_blks_built_time_skew` metric.
- Added p2p `bloomfilter_hit_rate` metrics for the P-chain, X-chain, C-chain atomic, and C-chain mempools.

### Fixes

- Fixed HTTP2 update request handling.
- Fixed plugin handling of special errors during HTTP request handling.
- Fixed plugin forwarding of HTTP headers.
- Fixed C-chain shared memory UTXO parsing by the Primary Network Wallet.

### What's Changed

- chore: move windows from tier 3 to unsupported by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4031
- [tmpnet] Run kube load test under a service account to validate RBAC by @maru-ava in https://github.com/ava-labs/avalanchego/pull/4030
- Add block timeskew metric by @yacovm in https://github.com/ava-labs/avalanchego/pull/4034
- refactor: use maps.Copy for cleaner map handling by @gopherorg in https://github.com/ava-labs/avalanchego/pull/4042
- Fix bug in rpcchainvm where `io.EOF` is not propagated correctly by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4035
- chore: fix some minor issues in the comments by @alongdate in https://github.com/ava-labs/avalanchego/pull/4045
- Update golangci-lint to v2  by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4037
- Simplex VerifiedBlock and BlockDeserializer by @samliok in https://github.com/ava-labs/avalanchego/pull/4009
- Load Framework 2 by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/3996
- Rename test_util -> util_test  by @samliok in https://github.com/ava-labs/avalanchego/pull/4054
- Replace toEngine channel with a dedicated subscription API by @yacovm in https://github.com/ava-labs/avalanchego/pull/3999
- Ensure that headers added by the server are propagated by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4060
- Update coreth to v0.15.3-rc.0 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4062
- Add default validator set function to snowtest.context by @JonathanOppenheimer in https://github.com/ava-labs/avalanchego/pull/4065
- Fix flake in TestNotifierReSubscribeAtPrefChange by @yacovm in https://github.com/ava-labs/avalanchego/pull/4067
- Support header-based routing by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4036
- Simplify json-rpc client handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4079
- Update coreth to v0.15.3-rc.1 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4080
- feat: add default flag options by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4044
- chore: fix some function names in comment by @shangchenglumetro in https://github.com/ava-labs/avalanchego/pull/4053
- chore: fix some inconsistent comments by @jingchanglu in https://github.com/ava-labs/avalanchego/pull/4068
- refactor: use the built-in max/min to simplify the code by @socialsister in https://github.com/ava-labs/avalanchego/pull/4087
- Comm Component for Simplex by @samliok in https://github.com/ava-labs/avalanchego/pull/3998
- ci(load-tests): add GitHub Actions workflow for load testing on k8s by @Elvis339 in https://github.com/ava-labs/avalanchego/pull/4051
- Update run monitored tmpnet cmd to make dashboard param configurable by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4089
- Add WritePrometheusServiceDiscoveryConfigFile helper by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/4072
- [nix] Update to 25.05 by @maru-ava in https://github.com/ava-labs/avalanchego/pull/4050
- chore: replace polling with subscription-based acceptance by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4059
- Add Verification + Tracking Logic To Simplex Blocks by @samliok in https://github.com/ava-labs/avalanchego/pull/4052
- Fix wrong pkg import for Primary Wallet by @0xJohnnyGault in https://github.com/ava-labs/avalanchego/pull/4096
- [tmpnet] Source tmpnet defaults from a configmap by @maru-ava in https://github.com/ava-labs/avalanchego/pull/4057
- Throttle platformVM block building until normal ops by @yacovm in https://github.com/ava-labs/avalanchego/pull/4097
- fix: add throttle to error retries by @alarso16 in https://github.com/ava-labs/avalanchego/pull/4098
- Measure bloom filter AppGossip hit rate by @yacovm in https://github.com/ava-labs/avalanchego/pull/4085

### New Contributors

- @gopherorg made their first contribution in https://github.com/ava-labs/avalanchego/pull/4042
- @alongdate made their first contribution in https://github.com/ava-labs/avalanchego/pull/4045
- @JonathanOppenheimer made their first contribution in https://github.com/ava-labs/avalanchego/pull/4065
- @shangchenglumetro made their first contribution in https://github.com/ava-labs/avalanchego/pull/4053
- @jingchanglu made their first contribution in https://github.com/ava-labs/avalanchego/pull/4068
- @0xJohnnyGault made their first contribution in https://github.com/ava-labs/avalanchego/pull/4096

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.13.2...v1.13.3

## [v1.13.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.2)

This version is backwards compatible to [v1.13.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0). It is optional, but encouraged.

The plugin version is updated to `41` all plugins must update to be compatible.

### APIs

- Added initial support for HTTP2 connections into VMs
- Removed native support for gzip compression of HTTP requests

### Fixes

- Fixed message timeout handling on L1s configured with `validatorOnly=true`
- Fixed segfault on ARM64 when profiling is enabled

### What's Changed

- chore(tests/load): C-chain load testing by @qdm12 in https://github.com/ava-labs/avalanchego/pull/3914
- Improve comments on message Ops by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3987
- Remove requestID expectation for UnrequestedOps by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3989
- Add Simplex Messages To p2p.proto by @samliok in https://github.com/ava-labs/avalanchego/pull/3976
- [tmpnet] Enable exclusive scheduling by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3988
- [testing] Add local kube support for load tests by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/3986
- Add a label to XSVM tests by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3991
- [tmpnet] Avoid port forwarding when running in a kube cluster by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3997
- Add support for VM HTTP2 handlers by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3294
- Move HTTP2 routing information into headers by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4001
- Clarify field names for simplex p2p messages by @samliok in https://github.com/ava-labs/avalanchego/pull/4002
- Remove gzip middleware from API server by @mpignatelli12 in https://github.com/ava-labs/avalanchego/pull/4005
- fix: allow for load tests to run in-cluster by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4003
- chore: update libevm version by @alarso16 in https://github.com/ava-labs/avalanchego/pull/4006
- Add support for XSVM grpc server reflection by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/4010
- fix: update log commands for stopping collectors by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/4011
- [tmpnet] Enure node config is saved on restart for both runtimes by @maru-ava in https://github.com/ava-labs/avalanchego/pull/4015
- BLS Components for Simplex by @samliok in https://github.com/ava-labs/avalanchego/pull/3993
- Capitalize secrets references by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4018
- build: update `libevm` to `v1.13.14-0.3.0.rc.1` by @alarso16 in https://github.com/ava-labs/avalanchego/pull/4023
- Allow internal messages from disallowed nodeIDs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/4024
- optimize historical range by @rrazvan1 in https://github.com/ava-labs/avalanchego/pull/3658

### New Contributors

- @mpignatelli12 made their first contribution in https://github.com/ava-labs/avalanchego/pull/4005
- @alarso16 made their first contribution in https://github.com/ava-labs/avalanchego/pull/4006

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.13.1...v1.13.2

## [v1.13.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.1)

This version is backwards compatible to [v1.13.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0). It is optional, but encouraged.

The plugin version is updated to `40` all plugins must update to be compatible.

### APIs

- Removed `avm.getAddressTxs` api
- Added L1 validators to `platformvm.GetCurrentValidators` client implementation

### Configs

- Removed `--tracing-enabled` and added `disabled` as an option to `--tracing-exporter-type`
- Removed AVM indexer configs
  - `index-transactions`
  - `index-allow-incomplete`

### What's Changed

- Export tmpnet functions for CLI interface by @felipemadero in https://github.com/ava-labs/avalanchego/pull/3727
- Use IPv4 addresses if possible by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3812
- [ci] Source shellcheck from nix instead of installing via script by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3811
- [ci] Use SHAs instead of tags for 3rd-party github actions by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3822
- [tmpnet] Add collector log path to readiness check log output by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3823
- Add context for errors in proposervm `repairAcceptedChainByHeight` by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3818
- [ci] Enable run-monitored-tmpnet-cmd to use a remote flake file by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3820
- Add context to errors in `avm` by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3821
- Remove block reindexing after Etna by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3813
- Update protobuf dependencies to the same version as nix packages by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3828
- update: platformvm config doc by @ashucoder9 in https://github.com/ava-labs/avalanchego/pull/3694
- Expose merkledb defaults by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3748
- [tmpnet] Add table of contents to README by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3837
- fix(metrics): fix c-chain metrics not reporting by @darioush in https://github.com/ava-labs/avalanchego/pull/3835
- [tmpnet] Update script to run instead of install by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3830
- [ci] Update to use commit SHAs for non-floating tags by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3834
- [ci] Configure action/setup-go to read golang version from go.mod by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3825
- [nix] Install protobuf codegen binaries in the dev shell by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3829
- [tmpnet] Remove obsolete readme content by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3827
- [docs] Document requirement to install modern bash on macos by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3841
- refactor: use the built-in max/min to simplify the code by @evenevent in https://github.com/ava-labs/avalanchego/pull/3844
- Update BLST to v0.3.14 to support Go 1.24 by @yacovm in https://github.com/ava-labs/avalanchego/pull/3846
- chore: allow individuals to extend `direnv` config by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3847
- [tmpnet] s/Network.ChainConfigs/Network.PrimaryChainConfigs/ by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3854
- Remove plugins/ from .gitignore by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3862
- fix: record wrong nil `err` by @tinyfoxy in https://github.com/ava-labs/avalanchego/pull/3851
- [tmpnet] Provide genesis, subnet and chain config via content flags by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3857
- Make sure inner state summary accept is called by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3831
- avoid tmpnet to create empty genesis on disk by @felipemadero in https://github.com/ava-labs/avalanchego/pull/3868
- Remove GetAddressTxs by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3872
- [tmpnet] Fixed faulty error handling on bootstrap failure by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3873
- Set `subnets.Config.ConsensusParameters` to serialize with omitempty by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3874
- [tmpnet] Update rpc version check to tolerate usage of `go run` by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3869
- Update coreth to v0.15.1-rc.0 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3875
- [tmpnet] Ensure Node has a reference to Network by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3870
- [tooling] Simplify avalanchego build script by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3861
- Simplify P-Chain block has changes check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3880
- [tmpnet] Switch back to using maps for subnet config by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3877
- [tmpnet] Refactor runtime configuration in preparation for kube by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3867
- [tooling] Add scripts that build+run tools and put them in the path by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3878
- [tooling] Add support for the Task (go-task) task runner by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3863
- [Docs] Fix links and make paths absolute by @martineckardt in https://github.com/ava-labs/avalanchego/pull/3885
- Remove unused constant checkIndexedFrequency by @yacovm in https://github.com/ava-labs/avalanchego/pull/3887
- [tmpnet] Unify start network flag usage between e2e and tmpnetctl by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3871
- [tmpnet] Avoid serializing the node data directory by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3881
- [tmpnet] Rename NodeProcess to ProcessRuntime by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3890
- wrap db in initDatabase with corruptable db by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3892
- [tmpnet] Switch FlagsMap from map[string]any to map[string]string by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3884
- [tmpnet] Ensure tmpnet methods always have a logger by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3893
- [tmpnet] Ensure all node runtime methods accept a context by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3894
- [tmpnet] Move WaitForHealthy from a function to a tmpnet.Node method by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3896
- Grant marun ownership of tooling configuration by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3895
- Bump golang.org/x/net from 0.36.0 to 0.38.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3889
- Fix typos by @omahs in https://github.com/ava-labs/avalanchego/pull/3908
- Fix typos and add missing hyphens in README files by @Dimitrolito in https://github.com/ava-labs/avalanchego/pull/3583
- Fix typos in iterator.go by @Marcofann in https://github.com/ava-labs/avalanchego/pull/3809
- Use EstimateBaseFee in e2e tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3782
- docs: fix flag name to `--proposervm-min-block-delay` by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3911
- fix rpcchainvm handling for arbitrary length http body by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3910
- Add git to nix packages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3912
- refactor: use the built-in max/min to simplify the code by @careworry in https://github.com/ava-labs/avalanchego/pull/3913
- Update codeowners by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3915
- [tmpnet] Delegate writing of the flag file to the runtime by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3897
- [tmpnet] Move monitoring label handling to node by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3898
- Move database creation to database factory package by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3899
- Update owner of CODEOWNERS by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3920
- update merkledb codeowners by @rrazvan1 in https://github.com/ava-labs/avalanchego/pull/3919
- chore: fix some comments by @standstaff in https://github.com/ava-labs/avalanchego/pull/3584
- Support UnmarshalJSON for `ExporterType` by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/3565
- Fix change/range proofs + simplify the code by @rrazvan1 in https://github.com/ava-labs/avalanchego/pull/3688
- [ci] Fix windows build job by reverting to use build script by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3921
- update: service.md for callouts by @ashucoder9 in https://github.com/ava-labs/avalanchego/pull/3832
- Reintroduce P-chain block reindexing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3883
- Bump bufbuild/buf-action from 1.1.0 to 1.1.1 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3855
- Bump github/codeql-action from 3.28.13 to 3.28.16 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3916
- Ensure HTTP headers are propagated through the rpcchainvm by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3917
- Document acp-118 message verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3925
- Add P-Chain state test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3924
- Document ACP-77 handling of 0 weight requests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3926
- Refactor cache implementations by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3239
- Fix `proposerMinBlockDelay` location in config doc for L1s by @federiconardelli7 in https://github.com/ava-labs/avalanchego/pull/3819
- Close stale issues and PRs by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3906
- Update wallet to report tx processing duration via event handlers by @marun in https://github.com/ava-labs/avalanchego/pull/3560
- Pretty print logged durations in E2E by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3930
- Reenable the upgrade test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3929
- Remove RequestBuildBlock on P-Chain Mempool by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3705
- Add test for proposervm BuildBlock after bootstrapping by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2876
- Add logging to corruptabledb closure by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3938
- Fix broken Buf documentation link in proto README by @GarmashAlex in https://github.com/ava-labs/avalanchego/pull/3936
- refactor: replace []byte(fmt.Sprintf) with fmt.Appendf by @findnature in https://github.com/ava-labs/avalanchego/pull/3932
- Add linkspector CI action by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3939
- Update minimum golang version to v1.23.9 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3940
- fix: validate allocations locked amount in genesis to prevent panic by @DracoLi in https://github.com/ava-labs/avalanchego/pull/3941
- [tmpnet] Enable runtime-specific restart behavior by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3882
- [tooling] Misc direnv changes by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3944
- Fully populate test context by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3943
- Use libevm instead of coreth by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3918
- [tmpnet] Define reusable flags for configuring kubernetes client access by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3945
- Fix flaky bootstrapping test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3955
- [tmpnet] Separate start of prometheus and promtail collectors by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3947
- Add L1 validators to getCurrentValidators response by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3843
- refactor: use slices.Contains to simplify code by @yetyear in https://github.com/ava-labs/avalanchego/pull/3952
- Update proposervm summary to roll forward only by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3950
- Remove dead code by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3966
- Add Granite to the `upgrade.Config` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3964
- refactor genesis building logic in avm and platformvm by @DracoLi in https://github.com/ava-labs/avalanchego/pull/3949
- [ci] Update dependabot to only propose security updates for github actions by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3969
- Bump bufbuild/buf-action from 1.1.1 to 1.1.4 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3971
- Bump github/codeql-action from 3.28.16 to 3.28.18 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3970
- Add Load Framework by @RodrigoVillar in https://github.com/ava-labs/avalanchego/pull/3942
- adds config.json for C-Chain during antithesis - json logs by @aleksandarknezevic in https://github.com/ava-labs/avalanchego/pull/3968
- [tmpnet] Ensure GetNodeURIs returns locally-accessible URIs to ensure kube compatibility by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3973
- refactor: use slices.Contains to simplify code by @pullmerge in https://github.com/ava-labs/avalanchego/pull/3974
- chore(deps): use coreth with geth-aligned ethclient package by @qdm12 in https://github.com/ava-labs/avalanchego/pull/3977
- Remove dead “Turtle’s Way HTTP/gRPC” link by @gap-editor in https://github.com/ava-labs/avalanchego/pull/3978
- Small cleanup in `App` and `Node` by @geoff-vball in https://github.com/ava-labs/avalanchego/pull/3962
- make GOPROXY overridable in constants.sh by @siphonelee in https://github.com/ava-labs/avalanchego/pull/3979
- Disallow slow sorting by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3981
- [tmpnet] Enable deployment to kube by @marun in https://github.com/ava-labs/avalanchego/pull/3615
- [tmpnet] Enable monitoring of nodes running in kube by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3794
- Bump coreth to include fix for large tx handling by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3984
- Align minCompatibleTime settings across TestNetwork and Network by @michaelkaplan13 in https://github.com/ava-labs/avalanchego/pull/3842
- Remove dead “Turtle’s Way HTTP/gRPC” link by @gap-editor in https://github.com/ava-labs/avalanchego/pull/3983

### New Contributors

- @evenevent made their first contribution in https://github.com/ava-labs/avalanchego/pull/3844
- @tinyfoxy made their first contribution in https://github.com/ava-labs/avalanchego/pull/3851
- @Dimitrolito made their first contribution in https://github.com/ava-labs/avalanchego/pull/3583
- @Marcofann made their first contribution in https://github.com/ava-labs/avalanchego/pull/3809
- @careworry made their first contribution in https://github.com/ava-labs/avalanchego/pull/3913
- @standstaff made their first contribution in https://github.com/ava-labs/avalanchego/pull/3584
- @RodrigoVillar made their first contribution in https://github.com/ava-labs/avalanchego/pull/3565
- @federiconardelli7 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3819
- @GarmashAlex made their first contribution in https://github.com/ava-labs/avalanchego/pull/3936
- @findnature made their first contribution in https://github.com/ava-labs/avalanchego/pull/3932
- @yetyear made their first contribution in https://github.com/ava-labs/avalanchego/pull/3952
- @aleksandarknezevic made their first contribution in https://github.com/ava-labs/avalanchego/pull/3968
- @pullmerge made their first contribution in https://github.com/ava-labs/avalanchego/pull/3974
- @gap-editor made their first contribution in https://github.com/ava-labs/avalanchego/pull/3978
- @geoff-vball made their first contribution in https://github.com/ava-labs/avalanchego/pull/3962
- @siphonelee made their first contribution in https://github.com/ava-labs/avalanchego/pull/3979

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.13.0...v1.13.1

## [v1.13.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0)

This upgrade consists of the following Avalanche Community Proposal (ACP):
- [ACP-176](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md) Dynamic EVM Gas Limits and Price Discovery Updates

The ACP in this upgrade goes into effect at 11 AM ET (3 PM UTC) on Tuesday, April 8th, 2025 on Mainnet.

**All Fortuna supporting Mainnet nodes should upgrade before 11 AM ET, April 8th 2025.**

The plugin version is unchanged at `39` and is compatible with version `v1.12.2`.

### APIs

- Added ProposerVM block timestamp metrics: `avalanche_proposervm_last_accepted_timestamp`
- Added network health check to alert if a primary network validator has no ingress connections. Runs a configurable time after startup or 10 minutes by default.

### Configs

- Added:
  - `--proposervm-min-block-delay`
  - `--network-no-ingress-connections-grace-period` to configure how long after startup it is expected for a Mainnet validator to have received an ingress connection.

### What's Changed

- Implement traversal based early termination by @yacovm in https://github.com/ava-labs/avalanchego/pull/3337
- Add networked-signer tests by @richardpringle in https://github.com/ava-labs/avalanchego/pull/3613
- Remove unused code by @yacovm in https://github.com/ava-labs/avalanchego/pull/3682
- chore(proposervm): timestamp metrics for block acceptance by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3680
- remove dependency of validator.State from BitSetSignature.Verify by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3679
- [docker] Optimize build time by copying and downloading deps first by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3683
- fix: broken link in README.md by @DeVikingMark in https://github.com/ava-labs/avalanchego/pull/3681
- [docker] Silence remaining InvalidDefaultArgInFrom warnings by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3684
- [tmpnet] Update subnet configuration in README by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3686
- testing: improve e2e test bootstrapping by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3690
- [tmpnet] Update URI and StakingAddress usage in support of kube by @marun in https://github.com/ava-labs/avalanchego/pull/3665
- [tmpnet] Re-enable reuse of dynamically allocated API ports by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3697
- fix spelling issues  by @futreall in https://github.com/ava-labs/avalanchego/pull/3700
- fix: correct typos in parser.go and tmpnet documentation by @avorylli in https://github.com/ava-labs/avalanchego/pull/3712
- fix: typos in documentation files by @maximevtush in https://github.com/ava-labs/avalanchego/pull/3710
- Fail fast in tests if avalancheGo executable isn't an absolute path by @yacovm in https://github.com/ava-labs/avalanchego/pull/3707
- [ci] Fix metrics link annotation emitted for e2e and upgrade jobs by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3713
- Remove Mock Mempool by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3687
- cleanup(tmpnet): resolve chainconfig post-etna TODO by @darioush in https://github.com/ava-labs/avalanchego/pull/3720
- Update to go 1.23.6 by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3722
- docs: Fix grammatical errors and improve clarity in documentation and comments by @VolodymyrBg in https://github.com/ava-labs/avalanchego/pull/3716
- [testing] Replace script-based tool installation with nix by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3691
- Remove unnecessary function by @richardpringle in https://github.com/ava-labs/avalanchego/pull/3723
- bump coreth to master by @darioush in https://github.com/ava-labs/avalanchego/pull/3724
- Make `bls.Signer` api fallible by @richardpringle in https://github.com/ava-labs/avalanchego/pull/3696
- Add comment to seemingly dead code by @richardpringle in https://github.com/ava-labs/avalanchego/pull/3721
- Bump coreth by @richardpringle in https://github.com/ava-labs/avalanchego/pull/3728
- Comment on the need for `CGO_ENABLED=1` to support cross-compilation by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3735
- [ci] Update to golangci-lint version compatible with go 1.23 by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3739
- [testing] Provide more logging context for SynchronizedBeforeSuite by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3741
- refactor: export PeerSample by @Elvis339 in https://github.com/ava-labs/avalanchego/pull/3745
- [antithesis] Set AVAGO_PLUGIN_DIR for VM images by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3751
- [ci] Drop support for Ubuntu 20.04 by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3737
- Fix spelling errors in `majority.go`, `minority.go`, `compressor.go`, and `logger.go` by @tomasandroil in https://github.com/ava-labs/avalanchego/pull/3738
- [ci] Simplify tmpnet monitoring action by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3736
- Remove apostrophe from Dockerfile comments by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3706
- fix error 404 link README.md by @futreall in https://github.com/ava-labs/avalanchego/pull/3750
- fix: typos in documentation files by @leopardracer in https://github.com/ava-labs/avalanchego/pull/3733
- Add With and WithOptions receivers to the Logger interface by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3729
- [tmpnet] Minimize duration of tx acceptance for e2e testing by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3685
- Remove unused `ForceCreateChain` function from `testManager` by @strmfos in https://github.com/ava-labs/avalanchego/pull/3755
- chore: make function comments match function names by @rustco in https://github.com/ava-labs/avalanchego/pull/3757
- L1 validator eviction block validity by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3758
- Add ACP-176 e2e tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3749
- [tmpnet] Deploy collectors with golang to simplify cross-repo use by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3692
- fix spelling issues config_test.go by @futreall in https://github.com/ava-labs/avalanchego/pull/3760
- [tmpnet] Add check for collection of logs and metrics to custom github action by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3740
- Deprecate the `snow.Context.Lock` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3762
- Name F-Upgrade Fortuna by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3761
- Add CodecID to ICM README by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3759
- Update CODEOWNERS s/marun/maru-ava/ by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3768
- Support caller-defined namespaces in merkledb by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3747
- Fix empty standard block check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3775
- Enable empty standard block check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3776
- Restrict ProposerVM P-chain height advancement by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3777
- Add canoto serialization support to the block context by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3709
- Remove support for AVM tx checksums by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3774
- [ci] Disable monitoring for jobs of PRs of fork branches by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3781
- Update db_test.go by @sky-coderay in https://github.com/ava-labs/avalanchego/pull/3765
- chore: fix some function names in comment by @tcpdumppy in https://github.com/ava-labs/avalanchego/pull/3773
- Implement ACP-118 Aggregator by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3394
- Print git commit version upon startup by @yacovm in https://github.com/ava-labs/avalanchego/pull/3771
- chore(nix): add darwin.apple_sdk.frameworks.Security by @darioush in https://github.com/ava-labs/avalanchego/pull/3769
- Add comment to SendConfig documenting how to broadcast by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3783
- refactor: use a more straightforward return value by @fuyangpengqi in https://github.com/ava-labs/avalanchego/pull/3726
- [ci] Stop emitting grafana link as an annotation by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3767
- Remove Etna activation banner by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3789
- Upgrade canoto to v0.13.3 by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3790
- Healthcheck for zero ingress connection count by @yacovm in https://github.com/ava-labs/avalanchego/pull/3719
- Timely halt Avalanche engine by @yacovm in https://github.com/ava-labs/avalanchego/pull/3792
- [docker] Update all images to debian12/bookworm by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3798
- [ci] Move monitoring check from github action to code by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3766
- [tmpnet] Start kind cluster with golang by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3780
- Fix flake TestIngressConnCount by @yacovm in https://github.com/ava-labs/avalanchego/pull/3799
- [tmpnet] Rename tmpnetctl main package from cmd to tmpnetctl by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3787
- Improve check-clean CI script by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3800
- Bump golang.org/x/net from 0.33.0 to 0.36.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3793
- Fix ACP-118 Aggregator Test Flake by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3801
- Canoto v0.15.0 upgrade by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3805
- [tmpnet] Fix README example for tmpnetctl script by @maru-ava in https://github.com/ava-labs/avalanchego/pull/3807
- Update coreth to v0.15.0-rc.0 by @darioush in https://github.com/ava-labs/avalanchego/pull/3808

### New Contributors

- @maru-ava made their first contribution in https://github.com/ava-labs/avalanchego/pull/3683
- @DeVikingMark made their first contribution in https://github.com/ava-labs/avalanchego/pull/3681
- @futreall made their first contribution in https://github.com/ava-labs/avalanchego/pull/3700
- @avorylli made their first contribution in https://github.com/ava-labs/avalanchego/pull/3712
- @maximevtush made their first contribution in https://github.com/ava-labs/avalanchego/pull/3710
- @VolodymyrBg made their first contribution in https://github.com/ava-labs/avalanchego/pull/3716
- @Elvis339 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3745
- @tomasandroil made their first contribution in https://github.com/ava-labs/avalanchego/pull/3738
- @leopardracer made their first contribution in https://github.com/ava-labs/avalanchego/pull/3733
- @strmfos made their first contribution in https://github.com/ava-labs/avalanchego/pull/3755
- @rustco made their first contribution in https://github.com/ava-labs/avalanchego/pull/3757
- @sky-coderay made their first contribution in https://github.com/ava-labs/avalanchego/pull/3765
- @tcpdumppy made their first contribution in https://github.com/ava-labs/avalanchego/pull/3773
- @fuyangpengqi made their first contribution in https://github.com/ava-labs/avalanchego/pull/3726

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.12.2...v1.13.0

## [v1.12.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.2)

This version is backwards compatible to [v1.12.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0). It is optional, but encouraged.

The plugin version is updated to `39` all plugins must update to be compatible.

**This release removes the support for the long deprecated Keystore API. Any users still relying on the keystore API will not be able to update to this version, or any later versions, of Avalanchego until their dependency on the keystore API has been removed.**

### APIs

- Deprecated:
  - `info.GetTxFee`
- Added:
  - `avm.GetTxFee`
  - `platform.getValidatorFeeConfig`
  - `platform.getValidatorFeeState`
  - `validationID` field to `platform.getL1Validator` results
  - L1 validators to `platform.getCurrentValidators`
- Removed:
  - `StakeAmount` field from `platform.getCurrentValidators` results
  - `keystore.createUser`
  - `keystore.deleteUser`
  - `keystore.listUsers`
  - `keystore.importUser`
  - `keystore.exportUser`
  - `avm.createAddress`
  - `avm.createFixedCapAsset`
  - `avm.createNFTAsset`
  - `avm.createVariableCapAsset`
  - `avm.export`
  - `avm.exportKey`
  - `avm.import`
  - `avm.importKey`
  - `avm.listAddresses`
  - `avm.mint`
  - `avm.mintNFT`
  - `avm.send`
  - `avm.sendMultiple`
  - `avm.sendNFT`
  - `wallet.send`
  - `wallet.sendMultiple`
  - `platform.exportKey`
  - `platform.listAddresses`

### Configs

- Removed static fee config flags
  - `--create-subnet-tx-fee`
  - `--transform-subnet-tx-fee`
  - `--create-blockchain-tx-fee`
  - `--add-primary-network-validator-fee`
  - `--add-primary-network-delegator-fee`
  - `--add-subnet-validator-fee`
  - `--add-subnet-delegator-fee`
- Removed `--api-keystore-enabled`

### What's Changed

- [testing] Always use the go.mod version of ginkgo by @marun in https://github.com/ava-labs/avalanchego/pull/3618
- Bump antithesishq/antithesis-trigger-action from 0.5 to 0.6 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3620
- Fix typos in document files by @taozui472 in https://github.com/ava-labs/avalanchego/pull/3622
- [ci] Always use the specified go version by @marun in https://github.com/ava-labs/avalanchego/pull/3616
- Fix: quotation mark by @jasmyhigh in https://github.com/ava-labs/avalanchego/pull/3623
- refactor: move node configs to config/node by @darioush in https://github.com/ava-labs/avalanchego/pull/3600
- Update e2e tests and CI jobs for post-etna by @marun in https://github.com/ava-labs/avalanchego/pull/3614
- Replace AWM terminology in ReadMe with ICM  by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/3595
- fix: grammatical mistakes by @crStiv in https://github.com/ava-labs/avalanchego/pull/3625
- [testing] Update golangci-lint to latest version by @marun in https://github.com/ava-labs/avalanchego/pull/3617
- partial sync default info by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/3602
- Update stale comment on commitToDB by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3627
- coreth atomic pkg dependency by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3588
- Mark Meag as the owner of README files by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3635
- Update x/net to v0.33.0 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3636
- Fix codeowners to simplify PR review by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3637
- Add BLS healthcheck to communicate incorrect BLS key configuration by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3638
- chore(all): mocks generation improved by @qdm12 in https://github.com/ava-labs/avalanchego/pull/3628
- fix LRU sized cache: consistent size at element removal by @rrazvan1 in https://github.com/ava-labs/avalanchego/pull/3634
- Index API and AvalancheGo Configs Docs Fix by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/3632
- [ci] Migrate from buf-*-action to buf-action by @marun in https://github.com/ava-labs/avalanchego/pull/3639
- chore(ci): define Github labels as code with a workflow by @qdm12 in https://github.com/ava-labs/avalanchego/pull/3629
- feat(github): add "needs Go upgrade" label by @qdm12 in https://github.com/ava-labs/avalanchego/pull/3642
- [ci] Fix post-merge protobuf lint job breakage by @marun in https://github.com/ava-labs/avalanchego/pull/3644
- merkledb visualisations v1 (change proofs and range proofs) by @rrazvan1 in https://github.com/ava-labs/avalanchego/pull/3643
- Remove Static Fee Config by @samliok in https://github.com/ava-labs/avalanchego/pull/3610
- fix(ci): trigger labels workflow on push to master not main by @qdm12 in https://github.com/ava-labs/avalanchego/pull/3646
- X-Chain API fix by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/3654
- [ci] Rename {PROMETHEUS,LOKI}_ID to {PROMETHEUS,LOKI}_USERNAME by @marun in https://github.com/ava-labs/avalanchego/pull/3652
- chore: replaced faulty link by @Radovenchyk in https://github.com/ava-labs/avalanchego/pull/3649
- Add L1 validator fees API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3647
- Reintroduce the deprecated `info.getTxFee` API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3656
- remove x-chain api obsolete metadata  by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/3655
- Remove the Keystore API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3657
- Add F Upgrade Scaffolding. Post-Etna Cleanup by @michaelkaplan13 in https://github.com/ava-labs/avalanchego/pull/3672
- [testing] Fix instructions for triggering antithesis test runs by @marun in https://github.com/ava-labs/avalanchego/pull/3664
- [testing] Ensure run_prometheus.sh uses a writeable storage path by @marun in https://github.com/ava-labs/avalanchego/pull/3662
- Make snowman use snowflake directly instead of snowball by @yacovm in https://github.com/ava-labs/avalanchego/pull/3403
- chore: fix some typos by @chuangjinglu in https://github.com/ava-labs/avalanchego/pull/3670
- Bump antithesishq/antithesis-trigger-action from 0.6 to 0.7 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3667
- [ci] Use go env {GOOS,GOARCH} for os and arch detection by @marun in https://github.com/ava-labs/avalanchego/pull/3661
- Silence docker InvalidDefaultArgInFrom warnings by @marun in https://github.com/ava-labs/avalanchego/pull/3659
- add L1 support to getCurrentValidators API by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3564
- [docker] Enable image builds from git worktrees by @marun in https://github.com/ava-labs/avalanchego/pull/3660
- [tmpnet] Set an explicit `instance` label for logs and metrics by @marun in https://github.com/ava-labs/avalanchego/pull/3650
- [docker] Switch to kube-compatible plugin path for images by @marun in https://github.com/ava-labs/avalanchego/pull/3653
- [testing] Support direnv to simplify usage of test tooling by @marun in https://github.com/ava-labs/avalanchego/pull/3651

### New Contributors

- @taozui472 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3622
- @jasmyhigh made their first contribution in https://github.com/ava-labs/avalanchego/pull/3623
- @crStiv made their first contribution in https://github.com/ava-labs/avalanchego/pull/3625
- @qdm12 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3628
- @rrazvan1 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3634
- @Radovenchyk made their first contribution in https://github.com/ava-labs/avalanchego/pull/3649
- @chuangjinglu made their first contribution in https://github.com/ava-labs/avalanchego/pull/3670

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.12.1...v1.12.2

## [v1.12.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.1)

This version is backwards compatible to [v1.12.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0). It is optional, but encouraged.

The plugin version is unchanged at `38` and is compatible with version `v1.12.0`.

### Configs

- Added PebbleDB option `sync` which defaults to `true`

### Fixes

- Fixed P-chain mempool verification to disallow transactions that exceed the available chain capacity

### What's Changed

- Expose test network cfg by @cam-schultz in https://github.com/ava-labs/avalanchego/pull/3573
- encapsulate signer by @richardpringle in https://github.com/ava-labs/avalanchego/pull/3576
- use pebble nosync by default by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3581
- fix: feeState API call in docs by @ashucoder9 in https://github.com/ava-labs/avalanchego/pull/3596
- Format Service.MD by @samliok in https://github.com/ava-labs/avalanchego/pull/3599
- Verify tx gas isn't too large in VerifyTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3604
- Add already implemented merkledb.View to MerkleDB interface by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3593
- Add tempdir in for chain ctx data dir by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3594
- Improve block building and verification logging by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3605
- Bump golang.org/x/crypto from 0.26.0 to 0.31.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/3608

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.12.0...v1.12.1

## [v1.12.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)

This upgrade consists of the following Avalanche Community Proposals (ACPs):
- [ACP-77](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/77-reinventing-subnets/README.md) Reinventing Subnets
- [ACP-103](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/103-dynamic-fees/README.md) Add Dynamic Fees to the P-Chain
- [ACP-118](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/118-warp-signature-request/README.md) Warp Signature Interface Standard
- [ACP-125](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/125-basefee-reduction/README.md) Reduce C-Chain minimum base fee from 25 nAVAX to 1 nAVAX
- [ACP-131](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/131-cancun-eips/README.md) Activate Cancun EIPs on C-Chain and Subnet-EVM chains
- [ACP-151](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/151-use-current-block-pchain-height-as-context/README.md) Use current block P-Chain height as context for state verification

The changes in the upgrade go into effect at 12 AM ET (5 PM UTC) on Monday, December 16th, 2024 on Mainnet.

**All Etna supporting Mainnet nodes should upgrade before 12 AM ET, December 16th 2024.**

The plugin version is unchanged at `38` and is compatible with version `v1.11.13`.

### APIs

- Allowed `platform.issueTx` to be called, for non-ImportTx transactions, while partial syncing

### What's Changed

- Fix SubnetToL1ConversionData typo by @cam-schultz in https://github.com/ava-labs/avalanchego/pull/3555
- Refactor `logging.Format` to expose constants by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3561
- [testing] Switch to logging with zap by @marun in https://github.com/ava-labs/avalanchego/pull/3557
- Use JSON logs during Antithesis runs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3562
- Antithesis: Skip checks if tx confirmation fails by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3563
- chore: fix some function names in comment by @wanxiangchwng in https://github.com/ava-labs/avalanchego/pull/3566
- update api docs by @ashucoder9 in https://github.com/ava-labs/avalanchego/pull/3558
- Remove unused wallet interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3568
- Remove required fields from config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3569
- Remove redundant field in platformVM/network's Network by @yacovm in https://github.com/ava-labs/avalanchego/pull/3571
- Remove observedSubnetUptime from Info Docs by @samliok in https://github.com/ava-labs/avalanchego/pull/3575
- Allow issuing transactions when using partial-sync by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3570
- Add partial-sync support to the wallet by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3567

### New Contributors

- @wanxiangchwng made their first contribution in https://github.com/ava-labs/avalanchego/pull/3566
- @ashucoder9 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3558

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.13...v1.12.0

## [v1.11.13](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.13)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is updated to `38` all plugins must update to be compatible.

### APIs

- Added `platform.getL1Validator`
- Added `platform.getProposedHeight`
- Updated `platform.getValidatorsAt` to accept `"proposed"` as valid `height` input

### Configs

- Added P-chain configs
  - `"l1-weights-cache-size"`
  - `"l1-inactive-validators-cache-size"`
  - `"l1-subnet-id-node-id-cache-size"`

### Fixes

- Fixed metrics initialization in the RPCChainVM. This could cause crashes during startup if metrics was requested during VM initialization.
- Fixed compilations on macos 14.7 and higher
- Fixed avalanchego wallet usage with ledger v0.8.4
- Fixed missing `NodeIDs` argument in the `info.peers` client implementation
- Fixed `getSubnetID` state tracing

### What's Changed

- [testing] Double image build timeout for bootstrap monitor e2e by @marun in https://github.com/ava-labs/avalanchego/pull/3468
- [antithesis] Double the duration of sanity checks by @marun in https://github.com/ava-labs/avalanchego/pull/3475
- Properly initialize metrics in rpcchainVM by @yacovm in https://github.com/ava-labs/avalanchego/pull/3477
- Cleanup editorconfig by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3473
- Update avalanche ledger go package by @sukantoraymond in https://github.com/ava-labs/avalanchego/pull/3456
- [testing] Enable config of log format for bootstrap monitor by @marun in https://github.com/ava-labs/avalanchego/pull/3467
- cache signatures only in acp118 handler by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3474
- Introduce and use `database.WithDefault` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3478
- Evict recentlyAccepted blocks based on wall-clock time  by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3460
- fix improper use of FailNow in testing by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3479
- [ACP 151] Use current block's P-Chain height as context for verifying state of the inner block by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3459
- [tmpnet] Add --start-network to support hypersdk MODE=run by @marun in https://github.com/ava-labs/avalanchego/pull/3465
- [e2e] Check network health after bootstrap checks by @marun in https://github.com/ava-labs/avalanchego/pull/3466
- ACP-77: Add ConversionID to state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3481
- Make bootstrapping handle its own timeouts by @yacovm in https://github.com/ava-labs/avalanchego/pull/3410
- Wrap `TestDiffExpiry` sub-tests in `t.Run` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3483
- Move RPC metrics registration after its client's initialization by @yacovm in https://github.com/ava-labs/avalanchego/pull/3488
- database: add applicable dbtests for linkeddb by @darioush in https://github.com/ava-labs/avalanchego/pull/3486
- Add SoV Excess to P-chain state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3482
- Remove deprecated X-chain pubsub server by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3490
- Update SoV struct to align with latest ACP-77 spec by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3492
- Register VM and snowman metrics after chain creation by @yacovm in https://github.com/ava-labs/avalanchego/pull/3489
- Skip Flaky Test by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3495
- Add request to update `releases.md` in PR template by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3476
- ACP-77: Update P-chain state staker tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3494
- ACP-77: Write subnet public key diffs to state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3487
- Add `Deregister` to `metrics.MultiGatherer` interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3498
- ACP-77: Add subnetIDNodeID struct by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3499
- Use subnet public key diffs after Etna is activated by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3502
- Split `writeCurrentStakers` into multiple functions by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3500
- [tmpnet] Refactor bootstrap monitor kubernetes functions for reuse by @marun in https://github.com/ava-labs/avalanchego/pull/3446
- Add NumSubnets to the validator manager interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3504
- Clarify partial sync flag by @michaelkaplan13 in https://github.com/ava-labs/avalanchego/pull/3505
- Update BLST to v0.3.13 by @yacovm in https://github.com/ava-labs/avalanchego/pull/3506
- Restrict public keys prior to TLS handshake by @yacovm in https://github.com/ava-labs/avalanchego/pull/3501
- ACP-77: Filter the inactive validator from block proposals and tx gossip by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3509
- [testing] Enable bootstrap testing of partial sync by @marun in https://github.com/ava-labs/avalanchego/pull/3508
- Rename `constantsAreUnmodified` to `immutableFieldsAreUnmodified` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3513
- Accept info.Peers args by @cam-schultz in https://github.com/ava-labs/avalanchego/pull/3515
- Return shallow copy of validator set in platformVM's validator manager by @yacovm in https://github.com/ava-labs/avalanchego/pull/3512
- Add `ValidatorWeightDiff` `Add` and `Sub` helpers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3514
- ACP-77: Add caching to SoV DB helpers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3516
- Add script to configure metrics and log collection from a local node by @marun in https://github.com/ava-labs/avalanchego/pull/3517
- ACP-77: Implement validator state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3388
- ACP-77: Reduce block gossip log level by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3519
- ACP-77: Implement ids.ID#Append by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3518
- ACP-103: Document and update genesis test fee configs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3520
- ACP-77: Deactivate SoVs without sufficient fees by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3412
- ACP-77: Allow legacy validator removal after conversion by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3521
- ACP-77: Refactor e2e test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3522
- ACP-77: Update `ConvertSubnetTx` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3397
- Remove stutter in P-chain wallet builder by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3524
- Clarify EndAccumulatedFee comment by @michaelkaplan13 in https://github.com/ava-labs/avalanchego/pull/3523
- [tmpnet] Misc cleanup for monitoring tooling by @marun in https://github.com/ava-labs/avalanchego/pull/3527
- Remove P-chain txsmock package by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3528
- Unexport all P-Chain visitors by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3525
- Standardize P-Chain tx visitor order by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3529
- ACP-77: Implement `RegisterSubnetValidatorTx` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3420
- ACP-77: Refactor P-Chain configs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3533
- Add additional BLS benchmarks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3538
- ACP-77: Refactor subnet auth verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3537
- ACP-77: Implement `SetSubnetValidatorWeightTx` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3421
- Rename error to be more generic by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3543
- fix getSubnetIDTag in traced state by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3542
- Add `platform.getSubnetOnlyValidator` API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3540
- Add SoV deactivation owner support to the P-chain wallet by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3541
- ACP-77: Implement Warp message verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3423
- ACP-77: Current validators API for SoV by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3404
- ACP-77: Implement Warp message signing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3428
- ACP-77: Implement IncreaseBalanceTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3429
- ACP-77: Implement DisableSubnetValidatorTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3440
- Improve P-Chain error messages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3536
- Add Etna logging by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3454
- Add Etna P-chain metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3458
- Clarify benched field by @samliok in https://github.com/ava-labs/avalanchego/pull/3545
- [tmpnet] Watch for and report FATAL log entries on node startup by @marun in https://github.com/ava-labs/avalanchego/pull/3535
- Allow non primary network validators to request all peers by @cam-schultz in https://github.com/ava-labs/avalanchego/pull/3491
- Add `platform.getProposedHeight` API by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3530
- Follow ACP-77 naming conventions by @michaelkaplan13 in https://github.com/ava-labs/avalanchego/pull/3546
- ACP-103: Finalize complexity calculations by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3548
- ACP-103: Finalize parameterization by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3549
- Add "proposed" optional flag to `getValidatorsAt` by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3531
- Fix json parsing of GetValidatorsAtArgs by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3551

### New Contributors

- @sukantoraymond made their first contribution in https://github.com/ava-labs/avalanchego/pull/3456
- @samliok made their first contribution in https://github.com/ava-labs/avalanchego/pull/3545

## [v1.11.11](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.11)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is updated to `37` all plugins must update to be compatible.

### APIs

- Updated JSON marshalling of the `Memo` field to follow best practices
- Added `info.upgrades`
- Added `platform.getFeeConfig`
- Added `platform.getFeeState`
- Deprecated subnet uptimes
  - `info.uptimes` with non-primary network subnetIDs is deprecated
  - `info.peers` `observedSubnetUptimes` is deprecated
  - `platform.getCurrentValidators` `uptime` and `connected` are deprecated for non-primary network subnetIDs.
  - `avalanche_network_node_subnet_uptime_weighted_average` metric is deprecated
  - `avalanche_network_node_subnet_uptime_rewarding_stake` metric is deprecated
- Added `avalanche_network_tracked_peers` metric
- Added `avalanche_network_tracked_subnets` metric
- Removed `avalanche_network_tracked_ips` metric
- Added disconnected validators to the health check result


### Configs

- Added upgrade config
  - `--upgrade-file`
  - `--upgrade-file-content`
- Added dynamic fees config
  - `--dynamic-fees-bandwidth-weight`
  - `--dynamic-fees-read-weight`
  - `--dynamic-fees-write-weight`
  - `--dynamic-fees-compute-weight`
  - `--dynamic-fees-max-gas-capacity`
  - `--dynamic-fees-max-gas-per-second`
  - `--dynamic-fees-target-gas-per-second`
  - `--dynamic-fees-min-gas-price`
  - `--dynamic-fees-excess-conversion-constant`

### Fixes

- Fixed panic when tracing is enabled
- Removed duplicate block signature verifications during bootstrapping
- Fixed racy timer clearing in message throttling

### What's Changed

- [ci] Remove defunct network outage sim workflow by @marun in https://github.com/ava-labs/avalanchego/pull/3234
- chore: allow test-only imports in `*test` and `/tests/**` packages by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3229
- Add benchmarks for add and sub fee dimensions by @abi87 in https://github.com/ava-labs/avalanchego/pull/3222
- Remove deadcode by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3086
- Parallelize BatchedParseBlock by @yacovm in https://github.com/ava-labs/avalanchego/pull/3227
- [ci] Lint on non-test code importing packages from /tests by @marun in https://github.com/ava-labs/avalanchego/pull/3214
- Merge unlocked stake outputs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3231
- ACP 118 reference implementation by @cam-schultz in https://github.com/ava-labs/avalanchego/pull/3218
- Storage OpenBSD/adJ by @vtamara in https://github.com/ava-labs/avalanchego/pull/2809
- Remove unused error from fee calculator creation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3245
- Rename Transitive snowman to Engine snowman by @yacovm in https://github.com/ava-labs/avalanchego/pull/3244
- Simplify static fee calculations by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3240
- Remove targetBlockSize arg by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3249
- Add dynamic fees config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3250
- Remove unused Samplers by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3219
- Inline `verifier` struct creation by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3252
- Add fee.State to P-chain state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3248
- Fix comparison comment in snowflake algorithms by @yacovm in https://github.com/ava-labs/avalanchego/pull/3256
- Add network upgrade config by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3207
- [vms/platformvm] Add `VerifyWithContext` to `Block`s by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3236
- [ci] Switch to v2 of docker compose plugin by @marun in https://github.com/ava-labs/avalanchego/pull/3259
- Minimize signature verification when bootstrapping by @yacovm in https://github.com/ava-labs/avalanchego/pull/3255
- [vms/platformvm] Add tracking of a Subnet manager by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3126
- Remove trackedSubnet check for explicitly named peers in network.Send() by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3258
- refactor: introduce `*test` packages in lieu of `//go:build test` by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3238
- [e2e] Enhance post-test bootstrap checks by @marun in https://github.com/ava-labs/avalanchego/pull/3253
- [e2e] Abstract usage of ginkgo with a new test context by @marun in https://github.com/ava-labs/avalanchego/pull/3254
- Update code owners by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3262
- [antithesis] Refactor image build for reuse by other repos by @marun in https://github.com/ava-labs/avalanchego/pull/3198
- Expose upgrade config in the info API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3266
- [antithesis] Ensure references to pushed images are qualified by @marun in https://github.com/ava-labs/avalanchego/pull/3264
- Fix spelling by @nnsW3 in https://github.com/ava-labs/avalanchego/pull/3267
- refactor: rename `*test.Test*` identifiers by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3260
- Separate e2e tests by etna activation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3268
- Implement P-chain ACP-103 complexity calculations by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3209
- Implement dynamic fee calculator by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3211
- [tmpnet] Add Network.GetNetworkID() to get ID of a running network by @marun in https://github.com/ava-labs/avalanchego/pull/3269
- Disable `TransformSubnetTx` post-Etna by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3152
- [tmpnet] Fail node health check if node is not running by @marun in https://github.com/ava-labs/avalanchego/pull/3274
- [tmpnet] Enable network restart to simplify iteration by @marun in https://github.com/ava-labs/avalanchego/pull/3272
- Add StoppedTimer helper by @marun in https://github.com/ava-labs/avalanchego/pull/3280
- Fix race in timer stoppage by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3281
- [tmpnet] Add check for vm binaries to network and node start by @marun in https://github.com/ava-labs/avalanchego/pull/3273
- Refactor P-chain Builder by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3282
- chore: fix some comments by @drawdrop in https://github.com/ava-labs/avalanchego/pull/3289
- Update write path of tmpnet subnet config by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3290
- add network upgrades to chain ctx by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3283
- Implement dynamic fee builder by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3232
- bump coreth past upgrade schedule refactor by @darioush in https://github.com/ava-labs/avalanchego/pull/3278
- Remove cross-chain requests by @darioush in https://github.com/ava-labs/avalanchego/pull/3277
- wallet: obtain subnet owners by using P-Chain's getSubnet API call by @felipemadero in https://github.com/ava-labs/avalanchego/pull/3247
- [antithesis] Add tmpnet support to workloads to simplify development by @marun in https://github.com/ava-labs/avalanchego/pull/3215
- Refactor e2e tests for P-chain tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3295
- Fix e2e tests to support dynamic fees by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3296
- Improve error message of dynamic fee calculations by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3297
- Reduce dynamic fees variability to ease testing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3298
- deprecate uptime apis by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3226
- [antithesis] Fix broken flag handling and improve image testing by @marun in https://github.com/ava-labs/avalanchego/pull/3299
- Add P-chain fee APIs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3286
- [tmpnet] Add support for checking rpcchainvm version compatibility by @marun in https://github.com/ava-labs/avalanchego/pull/3276
- Rename gas price calculation function by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3302
- Remove duplicate fork definitions by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3304
- Restrict `Owner` usage after a Subnet manager is set by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3147
- SoV networking support by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2951
- [antithesis] Enable custom plugin dir for subnet-evm by @marun in https://github.com/ava-labs/avalanchego/pull/3305
- Refactor state tests to always use initialized state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3310
- Remove mock for `Versions` interface by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3312
- Allow P-chain wallet to be used by the platformvm by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3314
- Remove crosschain leftovers by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3309
- Rename race condition image tags by @cam-schultz in https://github.com/ava-labs/avalanchego/pull/3311
- Add .String() to Fork testing utility by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3315
- [antithesis] Update schedule to make room for subnet-evm by @marun in https://github.com/ava-labs/avalanchego/pull/3317
- [tmpnet] Update monitoring urls from *-experimental to *-poc by @marun in https://github.com/ava-labs/avalanchego/pull/3306
- Add statetest to replace common test state initialization by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3319
- Rename `components/fee` pkg to `components/gas` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3321
- Remove mocks for `Staker` and `ScheduledStaker` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3322
- Move most mocks to sub-dirs by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3323
- [e2e] Simplify pre-funded key usage by @marun in https://github.com/ava-labs/avalanchego/pull/3011
- Move iterator implementations to `utils` pkg by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3320
- Remove duplicate genesis creations in P-chain unit tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3318
- Simplify P-Chain transaction creation in unit tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3327
- [tmpnet] Ensure nodes are stopped in the event of bootstrap failure by @marun in https://github.com/ava-labs/avalanchego/pull/3332
- Add tx complexity helper by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3334
- Fixed segfault when --tracing-enabled is set by @blenessy in https://github.com/ava-labs/avalanchego/pull/3330
- chore: remove deprecated `validatorstest.TestState` by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3301
- Enforce network config not including `PrimaryNetworkID` in `trackedSubnets` by @iansuvak in https://github.com/ava-labs/avalanchego/pull/3336
- Use wallet in `platformvm/block` tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3328
- [ci] Stop using setup-go-v3 by @marun in https://github.com/ava-labs/avalanchego/pull/3339
- Use wallet in `platformvm/txs` tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3333
- [ci] Configure buf-setup-action for check_generated_protobuf job with token by @marun in https://github.com/ava-labs/avalanchego/pull/3341
- Add P-chain dynamic fees execution by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3251
- Include disconnected validators in health message by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3344
- Print type of message sent in the verbose log by @yacovm in https://github.com/ava-labs/avalanchego/pull/3348
- Remove local chain config from e2e test by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3293
- Separate codec registries by upgrade by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3353
- Support unmarshalling of json byte slices by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3354
- Add explicit p-chain current validator check in AddValidator by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3355

### New Contributors

- @yacovm made their first contribution in https://github.com/ava-labs/avalanchego/pull/3227
- @cam-schultz made their first contribution in https://github.com/ava-labs/avalanchego/pull/3218
- @vtamara made their first contribution in https://github.com/ava-labs/avalanchego/pull/2809
- @iansuvak made their first contribution in https://github.com/ava-labs/avalanchego/pull/3258
- @nnsW3 made their first contribution in https://github.com/ava-labs/avalanchego/pull/3267
- @drawdrop made their first contribution in https://github.com/ava-labs/avalanchego/pull/3289
- @blenessy made their first contribution in https://github.com/ava-labs/avalanchego/pull/3330

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.10...v1.11.11

## [v1.11.10](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.10)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is updated to `36` all plugins must update to be compatible.

### APIs

- Renamed `avalanche_{vmName}_plugin_.*` metrics to `avalanche_{vmName}_.*`
- Renamed `avalanche_{vmName}_rpcchainvm_.*` metrics to `avalanche_rpcchainvm_.*`

### Fixes

- Updated local network validator start times
- Fixed block building timer recalculation when anyone can propose

### What's Changed

- Refactor rpcchainvm metrics registration by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3170
- Add example reward calculator usage by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3171
- Send AppErrors from p2p SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2753
- build(tests): require `//go:build test` tag if importing test packages outside of `_test.go` files by @ARR4N in https://github.com/ava-labs/avalanchego/pull/3173
- Include VM path in plugin version error by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3178
- [ci] Simplify ci monitoring with custom actions by @marun in https://github.com/ava-labs/avalanchego/pull/3161
- [vms/avm] Replace `strings.Replace` with `fmt.Sprintf` in tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3177
- Changes to support teleporter e2e tests by @feuGeneA in https://github.com/ava-labs/avalanchego/pull/3179
- Reduce usage of `getBlock` in consensus by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3151
- [ci] Enable run-monitored-tmpnet-cmd reuse by other repos by @marun in https://github.com/ava-labs/avalanchego/pull/3186
- Restructured fee calculator API by @abi87 in https://github.com/ava-labs/avalanchego/pull/3145
- P-Chain: Block-level fee Calculator by @abi87 in https://github.com/ava-labs/avalanchego/pull/3032
- [ci] Allow antithesis test setups to be triggered independently by @marun in https://github.com/ava-labs/avalanchego/pull/3183
- [antithesis] Fix image version separator in triggering workflows by @marun in https://github.com/ava-labs/avalanchego/pull/3191
- Remove `block.Status` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3158
- [antithesis] Refactor compose config generation to simplify reuse by @marun in https://github.com/ava-labs/avalanchego/pull/3184
- [antithesis] Add schedule for workflows by @marun in https://github.com/ava-labs/avalanchego/pull/3192
- Update `golangci-lint` to `v1.59.1` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3195
- [ci] Ensure monitoring action compatibility for other repos by @marun in https://github.com/ava-labs/avalanchego/pull/3193
- chore: fix some comments for struct field by @linghuying in https://github.com/ava-labs/avalanchego/pull/3194
- [antithesis] Configure workload history by @marun in https://github.com/ava-labs/avalanchego/pull/3196
- [vms/proposervm] Set build block time correctly  when anyone can propose by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3197
- chore: fix comment by @polymaer in https://github.com/ava-labs/avalanchego/pull/3201
- Make math.Add64 and math.Mul64 generic by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3205
- Implement ACP-103 fee package by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3203
- [antithesis] Fix job duration by @marun in https://github.com/ava-labs/avalanchego/pull/3206
- [vms/platformvm] `RegisterDUnsignedTxsTypes` -> `RegisterDurangoUnsignedTxsTypes` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3212
- chore: fix some comments by @yingshanghuangqiao in https://github.com/ava-labs/avalanchego/pull/3213
- Fix typos by @omahs in https://github.com/ava-labs/avalanchego/pull/3208
- Cleanup fee.staticCalculator by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3210
- typo by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/3220
- add getSubnet to p-chain api reference by @felipemadero in https://github.com/ava-labs/avalanchego/pull/3204
- [ci] Update fuzz workflows to target master branch by @marun in https://github.com/ava-labs/avalanchego/pull/3221
- Cleanup wallet tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3230
- Update local validator start time by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3224

### New Contributors

- @feuGeneA made their first contribution in https://github.com/ava-labs/avalanchego/pull/3179
- @linghuying made their first contribution in https://github.com/ava-labs/avalanchego/pull/3194
- @polymaer made their first contribution in https://github.com/ava-labs/avalanchego/pull/3201
- @yingshanghuangqiao made their first contribution in https://github.com/ava-labs/avalanchego/pull/3213
- @omahs made their first contribution in https://github.com/ava-labs/avalanchego/pull/3208

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.9...v1.11.10

## [v1.11.9](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.9)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is unchanged at `35` and is compatible with versions `v1.11.3-v1.11.8`.

### APIs

- Updated health metrics to use labels rather than namespaces
- Added consensus poll termination metrics

### Configs

- Added `--version-json` flag to output version information in json format

### Fixes

- Fixed incorrect WARN log that could previously be emitted during start on nodes with slower disks
- Fixed incorrect ERROR log that could previously be emitted if a peer tracking a subnet connects during shutdown
- Fixed ledger dependency on erased commit
- Fixed protobuf dependency to resolve compilation issues in some cases
- Fixed C-chain filename logging

### What's Changed

- Error driven snowflake multi counter by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3092
- [antithesis] Add ci jobs to trigger test runs by @marun in https://github.com/ava-labs/avalanchego/pull/3076
- bump ledger-avalanche dependency to current main branch by @felipemadero in https://github.com/ava-labs/avalanchego/pull/3115
- [antithesis] Fix image publication job by quoting default tag value by @marun in https://github.com/ava-labs/avalanchego/pull/3112
- [e2e] Fix excessively verbose output from virtuous test by @marun in https://github.com/ava-labs/avalanchego/pull/3116
- Remove .Status() from .IsPreferred() by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3111
- Add early termination metrics case by case by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/3093
- Update C-chain wallet context by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3118
- Standardize wallet tx acceptance polling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3110
- [antithesis] Remove assertions incompatible with fault injection by @marun in https://github.com/ava-labs/avalanchego/pull/3104
- Use health labels by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3122
- Remove `Decided` from the `Consensus` interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3123
- Remove .Status() from .Accepted() by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3124
- Refactor `event.Blocker` into `job.Scheduler` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3125
- Remove block lookup from `deliver` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3130
- [chains/atomic] Remove a nested if statement by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3135
- [vms/platformvm] Minor grammer fixes in `state` struct code comments by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3136
- bump protobuf (fixes some build issues) by @darioush in https://github.com/ava-labs/avalanchego/pull/3142
- Emit version in JSON format for --json-version by @marun in https://github.com/ava-labs/avalanchego/pull/3129
- Repackaged NextBlockTime and GetNextStakerChangeTime by @abi87 in https://github.com/ava-labs/avalanchego/pull/3134
- [vms/platformvm] Cleanup execution config tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3137
- [tmpnet] Enable bootstrap of subnets with disjoint validator sets by @marun in https://github.com/ava-labs/avalanchego/pull/3138
- Simplify dependency registration by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3139
- Replace `wasIssued` with `shouldIssueBlock` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3131
- Remove parent lookup from issue by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3132
- Remove status usage from consensus by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3140
- Fix bootstrapping warn log by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3156
- chore: fix some comment by @hattizai in https://github.com/ava-labs/avalanchego/pull/3144
- [ci] Add actionlint job by @marun in https://github.com/ava-labs/avalanchego/pull/3160
- check router is closing in requests by @ceyonur in https://github.com/ava-labs/avalanchego/pull/3157
- Use `ids.Empty` instead of `ids.ID{}` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3166
- Replace usage of utils.Err with errors.Join by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/3167

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.8...v1.11.9

## [v1.11.8](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.8)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is unchanged at `35` and is compatible with versions `v1.11.3-v1.11.7`.

### APIs

- Redesigned metrics to use labels rather than custom namespaces.

### What's Changed

- Remove avalanche metrics registerer from consensus context by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3087
- Remove rejection from `consensus.Add` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3084
- [vms/platformvm] Rename `txstest.Builder` to `txstest.WalletFactory` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2890
- Small metrics cleanup by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3088
- Fix race in test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3089
- Implement error driven snowflake hardcoded to support a single beta by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2978
- Replace all chain namespaces with labels by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3053
- add a metrics gauge for built block slot by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3048
- [ci] Switch to gh workers for arm64 by @marun in https://github.com/ava-labs/avalanchego/pull/3090
- [ci] Ensure focal arm64 builds all have their required dependencies by @marun in https://github.com/ava-labs/avalanchego/pull/3091
- X-chain - consolidate tx creation in unit tests by @abi87 in https://github.com/ava-labs/avalanchego/pull/2736
- Use netip.AddrPort rather than ips.IPPort by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3094

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.7...v1.11.8

## [v1.11.7](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.7)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is unchanged at `35` and is compatible with versions `v1.11.3-v1.11.6`.

### APIs

- Added peer's `trackedSubnets` that are not locally tracked to the response from `info.peers`

### Configs

- Changed the undocumented `pebble` option for `--db-type` to be `pebbledb` and documented the option

### Fixes

- Removed repeated DB compaction during bootstrapping that caused a significant regression in bootstrapping times
- Fixed C-Chain state-sync crash
- Fixed C-Chain state-sync ETA calculation
- Fixed Subnet owner reported by `platform.getSubnets` after a subnet's owner was rotated

### What's Changed

- Expose canonical warp formatting function by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3049
- Remove subnet filter from Peer.TrackedSubnets() by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2975
- Remove optional gatherer by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3052
- [vms/platformvm] Return the correct owner in `platform.GetSubnets` after transfer by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3054
- Add metrics client by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3057
- [vms/platformvm] Replace `GetSubnets` with `GetSubnetIDs` in `State` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3055
- Implement `constants.VMName` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3058
- [testing] Remove superfluous gomega dep by @marun in https://github.com/ava-labs/avalanchego/pull/3063
- [antithesis] Enable workload instrumentation by @marun in https://github.com/ava-labs/avalanchego/pull/3059
- Add pebbledb to docs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3061
- [ci] Remove perpetually failing govulncheck job by @marun in https://github.com/ava-labs/avalanchego/pull/3069
- Remove api namespace by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3066
- Remove unused metrics namespaces by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3062
- Only compact after executing a large number of blocks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3065
- Remove network namespace by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3067
- Remove db namespace by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3068
- Remove averager metrics namespace by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3072
- chore: fix function name by @stellrust in https://github.com/ava-labs/avalanchego/pull/3075
- Select metric by label in e2e tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3073
- [tmpnet] Bootstrap subnets with a single node by @marun in https://github.com/ava-labs/avalanchego/pull/3005
- [antithesis] Skip push for builder image by @marun in https://github.com/ava-labs/avalanchego/pull/3070
- Implement label gatherer by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3074

### New Contributors

- @stellrust made their first contribution in https://github.com/ava-labs/avalanchego/pull/3075

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.6...v1.11.7

## [v1.11.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.6)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is unchanged at `35` and is compatible with versions `v1.11.3-v1.11.5`.

### APIs

- Updated cache metrics:
  - `*_cache_put_sum` was replaced with `*_cache_put_time`
  - `*_cache_get_sum` was replaced with `*_cache_get_time`
  - `*_cache_hit` and `*_cache_miss` were removed and `*_cache_get_count` added a `result` label
- Updated db metrics:
  - `*_db_{method}_count` were replaced with `*_db_calls` with a `method` label
  - `*_db_{method}_sum` were replaced with `*_db_duration` with a `method` label
  - `*_db_{method}_size_count` were deleted
  - `*_db_{method}_size_sum` were replaced with `*_db_size` with a `method` label
- Updated p2p message compression metrics:
  - `avalanche_network_codec_{type}_{op}_{direction}_time_count` were replaced with `avalanche_network_codec_compressed_count` with `direction`, `op`, and `type` labels
- Updated p2p message metrics:
  - `avalanche_network_{op}_{io}` were replaced with `avalanche_network_msgs` with `compressed:"false"`, `io`, and `op` labels
  - `avalanche_network_{op}_{io}_bytes` were replaced with `avalanche_network_msgs_bytes` with `io` and `op` labels
  - `avalanche_network_{op}_compression_saved_{io}_bytes_sum` were replaced with `avalanche_network_msgs_bytes_saved` with `io` and `op` labels
  - `avalanche_network_{op}_compression_saved_{io}_bytes_count` were replaced with `avalanche_network_msgs` with `compressed:"true"`, `io`, and `op` labels
  - `avalanche_network_{op}_failed` were replaced with `avalanche_network_msgs_failed_to_send` with an `op` label
- Updated p2p sdk message metrics:
  - `*_p2p_{op}_count` were replaced with `*_p2p_msg_count` with an `op` label
  - `*_p2p_{op}_time` were replaced with `*_p2p_msg_time` with an `op` label
- Updated consensus message queue metrics:
  - `avalanche_{chainID}_handler_unprocessed_msgs_{op}` were replaced with `avalanche_{chainID}_handler_unprocessed_msgs_count` with an `op` label
  - `avalanche_{chainID}_handler_async_unprocessed_msgs_{op}` were replaced with `avalanche_{chainID}_handler_unprocessed_msgs_count` with an `op` label
- Updated consensus handler metrics:
  - `avalanche_{chainID}_handler_{op}_count` were replaced with `avalanche_{chainID}_handler_messages` with an `op` label
  - `avalanche_{chainID}_handler_{op}_msg_handling_count` was deleted
  - `avalanche_{chainID}_handler_{op}_msg_handling_sum` were replaced with `avalanche_{chainID}_handler_message_handling_time` with an `op` label
  - `avalanche_{chainID}_handler_{op}_sum` were replaced with `avalanche_{chainID}_handler_locking_time`
- Updated consensus sender metrics:
  - `avalanche_{chainID}_{op}_failed_benched` were replaced with `avalanche_{chainID}_failed_benched` with an `op` label
- Updated consensus latency metrics:
  - `avalanche_{chainID}_lat_{op}_count` were replaced with `avalanche_{chainID}_response_messages` with an `op` label
  - `avalanche_{chainID}_lat_{op}_sum` were replaced with `avalanche_{chainID}_response_message_latencies` with an `op` label
- Updated X-chain metrics:
  - `avalanche_X_vm_avalanche_{tx}_txs_accepted` were replaced with `avalanche_X_vm_avalanche_txs_accepted` with a `tx` label
- Updated P-chain metrics:
  - `avalanche_P_vm_{tx}_txs_accepted` were replaced with `avalanche_P_vm_txs_accepted` with a `tx` label
  - `avalanche_P_vm_{blk}_blks_accepted` were replaced with `avalanche_P_vm_blks_accepted` with a `blk` label

### Fixes

- Fixed performance regression while executing blocks in bootstrapping
- Fixed peer connection tracking in the P-chain and C-chain to re-enable tx pull gossip
- Fixed C-chain deadlock while executing blocks in bootstrapping after aborting state sync
- Fixed negative ETA while fetching blocks after aborting state sync
- Fixed C-chain snapshot initialization after state sync
- Fixed panic when running avalanchego in environments with an incorrectly implemented monotonic clock
- Fixed memory corruption when accessing keys and values from released pebbledb iterators
- Fixed prefixdb compaction when specifying a `nil` limit

### What's Changed

- Consolidate record poll by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2970
- Update metercacher to use vectors by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2979
- Reduce p2p sdk metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2980
- Use vectors in message queue metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2985
- Use vectors for p2p message metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2983
- Simplify gossip metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2984
- Use vectors for message handler metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2987
- Use vector in message sender by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2988
- Simplify go version maintenance by @marun in https://github.com/ava-labs/avalanchego/pull/2977
- Use vector for router latency metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2989
- Use vectors for accepted tx and block metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2990
- fix: version application error by @jujube in https://github.com/ava-labs/avalanchego/pull/2995
- Chore: fix some typos. by @hattizai in https://github.com/ava-labs/avalanchego/pull/2993
- Cleanup meterdb metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2991
- Cleanup compression metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2992
- Fix antithesis image publication by @marun in https://github.com/ava-labs/avalanchego/pull/2998
- Remove unused `Metadata` struct by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3001
- prefixdb: fix bug with Compact nil limit by @a1k0n in https://github.com/ava-labs/avalanchego/pull/3000
- Update go version to 1.21.10 by @marun in https://github.com/ava-labs/avalanchego/pull/3004
- vms/txs/mempool: unify avm and platformvm mempool implementations by @lebdron in https://github.com/ava-labs/avalanchego/pull/2994
- Use gauges for time metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3009
- Chore: fix typos. by @cocoyeal in https://github.com/ava-labs/avalanchego/pull/3010
- [antithesis] Refactor existing job to support xsvm test setup by @marun in https://github.com/ava-labs/avalanchego/pull/2976
- chore: fix some function names by @cartnavoy in https://github.com/ava-labs/avalanchego/pull/3015
- Mark nodes as connected to the P-chain networking stack by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2981
- [antithesis] Ensure images with a prefix are pushed by @marun in https://github.com/ava-labs/avalanchego/pull/3016
- boostrapper: compact blocks before iterating them by @a1k0n in https://github.com/ava-labs/avalanchego/pull/2997
- Remove pre-Durango networking checks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3018
- Repackaged upgrades times into upgrade package by @abi87 in https://github.com/ava-labs/avalanchego/pull/3019
- Standardize peer logs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3017
- Fix pebbledb memory corruption by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3020
- [vms/avm] fix linter error in benchmark : Use of weak random number generator by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3023
- Simplify sampler interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3026
- [build] Update linter version by @tsachiherman in https://github.com/ava-labs/avalanchego/pull/3024
- fix broken link. by @cocoyeal in https://github.com/ava-labs/avalanchego/pull/3028
- `gossipping` -> `gossiping` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3033
- [tmpnet] Ensure tmpnet compatibility with windows by @marun in https://github.com/ava-labs/avalanchego/pull/3002
- Fix negative ETA caused by rollback in vm.SetState by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3036
- [tmpnet] Enable single node networks by @marun in https://github.com/ava-labs/avalanchego/pull/3003
- P-chain - introducing fees calculators by @abi87 in https://github.com/ava-labs/avalanchego/pull/2698
- Change default staking key from RSA 4096 to secp256r1 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3025
- Fix ACP links by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3037
- Prevent unnecessary bandwidth from activated ACPs by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/3031
- [antithesis] Add test setup for xsvm by @marun in https://github.com/ava-labs/avalanchego/pull/2982
- [antithesis] Ensure node image is pushed by @marun in https://github.com/ava-labs/avalanchego/pull/3042
- Cleanup fee config passing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3043
- Fix typo fix by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3044
- Grab iterator at previously executed height by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3045
- Verify signatures during Parse by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/3046

### New Contributors

- @jujube made their first contribution in https://github.com/ava-labs/avalanchego/pull/2995
- @hattizai made their first contribution in https://github.com/ava-labs/avalanchego/pull/2993
- @a1k0n made their first contribution in https://github.com/ava-labs/avalanchego/pull/3000
- @lebdron made their first contribution in https://github.com/ava-labs/avalanchego/pull/2994
- @cocoyeal made their first contribution in https://github.com/ava-labs/avalanchego/pull/3010
- @cartnavoy made their first contribution in https://github.com/ava-labs/avalanchego/pull/3015
- @tsachiherman made their first contribution in https://github.com/ava-labs/avalanchego/pull/3023

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.5...v1.11.6

## [v1.11.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.5)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is unchanged at `35` and is compatible with versions `v1.11.3-v1.11.4`.

### APIs

- Renamed metric `avalanche_network_validator_ips` to `avalanche_network_tracked_ips`

### Configs

- Removed `--snow-virtuous-commit-threshold`
- Removed `--snow-rogue-commit-threshold`

### Fixes

- Fixed increased outbound PeerList messages when specifying custom bootstrap IDs
- Fixed CPU spike when disconnected from the network during bootstrapping fetching
- Fixed topological sort in vote calculation
- Fixed job dependency handling for transitively rejected blocks
- Prevented creation of unnecessary consensus polls during the issuance of a block

### What's Changed

- Remove duplicate metrics increment by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2926
- Optimize merkledb metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2927
- Optimize intermediateNodeDB.constructDBKey by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2928
- [vms/proposervm] Remove `getForkHeight()` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2929
- Improve logging of startup and errors in bootstrapping by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2933
- Add hashing interface to merkledb by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2930
- Assign instead of append to `keys` slice by @danlaine in https://github.com/ava-labs/avalanchego/pull/2932
- Remove uptimes from Pong messages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2936
- Enable creation of multi-arch docker images by @marun in https://github.com/ava-labs/avalanchego/pull/2914
- Improve networking README by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2937
- Specify golang patch version in go.mod by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2938
- Include consensus decisions into logs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2943
- CI: ensure image build job is compatible with merge queue by @marun in https://github.com/ava-labs/avalanchego/pull/2941
- Remove unused `validators.Manager` mock by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2944
- Split ManuallyTrack into ManuallyTrack and ManuallyGossip by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2940
- Sync primary network checkpoints during bootstrapping by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2752
- [ci] Add govulncheck job and update x/net as per its recommendation by @marun in https://github.com/ava-labs/avalanchego/pull/2948
- [tmpnet] Add network reuse to e2e fixture by @marun in https://github.com/ava-labs/avalanchego/pull/2935
- `e2e`: Add basic warp test with xsvm by @marun in https://github.com/ava-labs/avalanchego/pull/2043
- Improve bootstrapping peer selection by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2946
- Cleanup avalanche bootstrapping fetching by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2947
- Add manager validator set callbacks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2950
- chore: fix function names in comment by @socialsister in https://github.com/ava-labs/avalanchego/pull/2957
- [ci] Fix conditional guarding monitoring configuration by @marun in https://github.com/ava-labs/avalanchego/pull/2959
- Cleanup consensus engine tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2953
- Improve and test getProcessingAncestor by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2956
- Exit topological sort earlier by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2965
- Consolidate beta by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2949
- Abandon decided blocks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2968
- Bump bufbuild/buf-setup-action from 1.30.0 to 1.31.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2923
- Cleanup test block creation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2973

### New Contributors

- @socialsister made their first contribution in https://github.com/ava-labs/avalanchego/pull/2957

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.4...v1.11.5

## [v1.11.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.4)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is unchanged at `35` and is compatible with version `v1.11.3`.

### APIs

- Removed metrics for each chainID:
  - `avalanche_{chainID}_bs_eta_fetching_complete`
  - `avalanche_{chainID}_block_eta_execution_complete`
  - `avalanche_{chainID}_block_jobs_cache_get_count`
  - `avalanche_{chainID}_block_jobs_cache_get_sum`
  - `avalanche_{chainID}_block_jobs_cache_hit`
  - `avalanche_{chainID}_block_jobs_cache_len`
  - `avalanche_{chainID}_block_jobs_cache_miss`
  - `avalanche_{chainID}_block_jobs_cache_portion_filled`
  - `avalanche_{chainID}_block_jobs_cache_put_count`
  - `avalanche_{chainID}_block_jobs_cache_put_sum`
- Added finer grained tracing of merkledb trie construction and hashing
  - renamed `MerkleDB.view.calculateNodeIDs` to `MerkleDB.view.applyValueChanges`
  - Added `MerkleDB.view.calculateNodeChanges`
  - Added `MerkleDB.view.hashChangedNodes`

### Fixes

- Fixed p2p SDK handling of cancelled `AppRequest` messages
- Fixed merkledb crash recovery

### What's Changed

- Bump github.com/consensys/gnark-crypto from 0.10.0 to 0.12.1 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2862
- Push antithesis images by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2864
- Revert removal of legacy P-chain block parsing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2866
- `tmpnet`: Ensure nodes are properly detached from the parent process by @marun in https://github.com/ava-labs/avalanchego/pull/2859
- indicies -> indices by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2873
- Reindex P-chain blocks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2869
- Add detail to tmpnet metrics documentation by @marun in https://github.com/ava-labs/avalanchego/pull/2854
- docs migration by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/2845
- Implement interval tree to replace bootstrapping jobs queue by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2756
- Cleanup codec constants by @abi87 in https://github.com/ava-labs/avalanchego/pull/2699
- Update health API readme by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2875
- `tmpnet`: Improve subnet configuration by @marun in https://github.com/ava-labs/avalanchego/pull/2871
- Add tests for inefficient string formatting by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2878
- [vms/platformvm] Declare `maxPageSize` in `service.go` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2881
- [vms/platformvm] Use `wallet` sdk in `txstest.Builder` by @abi87 in https://github.com/ava-labs/avalanchego/pull/2751
- Optimize encodeUint by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2882
- [components/avax] Remove `AtomicUTXOManager` interface by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2884
- Remove merkledb codec struct by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2883
- [vms/platformvm] Minimize exported functions in `txstest` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2888
- `ci`: Skip monitoring if secrets are not present by @marun in https://github.com/ava-labs/avalanchego/pull/2880
- Optimize merkledb hashing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2886
- [vms/platformvm] Miscellaneous testing cleanups by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2891
- Move functions around so that encode and decode are next to each other by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2892
- Remove memory alloc from encodeDBNode by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2893
- Interval tree syncing integration by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2855
- Optimize hashing of leaf nodes by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2894
- Improve performance of marshalling small keys by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2895
- Improve tracing of merkledb trie updates by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2897
- Remove usage of bytes.Buffer and bytes.Reader by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2896
- Optimize key creation in hashing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2899
- Move bootstrapping queue out of common by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2856
- Conditionally allocate WaitGroup memory by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2901
- Reuse key buffers during hashing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2902
- Remove AddEphemeralNode by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2887
- Rename linkedhashmap package to `linked` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2907
- [tmpnet] Misc cleanup to support xsvm warp test PR by @marun in https://github.com/ava-labs/avalanchego/pull/2903
- Implement generic `linked.List` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2908
- Remove full message from error logs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2912
- Use generic linked list by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2909
- Avoid allocating new list entries by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2910
- Remove `linked.Hashmap` locking by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2911
- Fix MerkleDB crash recovery by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2913
- Remove cancellation for Send*AppRequest messages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2915
- Add `.Clear()` to `linked.Hashmap` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2917
- Allow pre-allocating `linked.Hashmap` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2918
- Fix comment and remove unneeded allocation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2919
- Implement `utils.BytesPool` to replace `sync.Pool` for byte slices by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2920
- Refactor `MerkleDB.commitChanges` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2921
- Remove value_node_db batch by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2922
- Remove memory allocations from merkledb iteration by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2925

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.3...v1.11.4

## [v1.11.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.3)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but encouraged.

The plugin version is updated to `35` all plugins must update to be compatible.

### APIs

- Removed:
  - `platform.GetPendingValidators`
  - `platform.GetMaxStakeAmount`

### Configs

- Removed avalanchego configs:
  - `network-peer-list-validator-gossip-size`
  - `network-peer-list-non-validator-gossip-size`
  - `network-peer-list-peers-gossip-size`
  - `network-peer-list-gossip-frequency`
  - `consensus-accepted-frontier-gossip-validator-size`
  - `consensus-accepted-frontier-gossip-non-validator-size`
  - `consensus-accepted-frontier-gossip-peer-size`
  - `consensus-on-accept-gossip-validator-size`
  - `consensus-on-accept-gossip-non-validator-size`
  - `consensus-on-accept-gossip-peer-size`
- Added P-chain, X-chain, and C-chain configs:
  - `push-gossip-percent-stake`

### Fixes

- Fixed p2p SDK validator sampling to only return connected validators

### What's Changed

- Cleanup BLS naming and documentation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2798
- Add BLS keys + signers config for local network by @Nuttymoon in https://github.com/ava-labs/avalanchego/pull/2794
- Remove double spaces by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2802
- [vms/platformvm] Remove `platform.getMaxStakeAmount` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2795
- Remove unused engine interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2811
- Cleanup Duplicate Transitive Constructor by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2812
- Update minimum golang version to v1.21.8 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2814
- Cleanup consensus metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2815
- Remove peerlist push gossip by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2791
- Remove bitmaskCodec by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2792
- Use `BaseTx` in P-chain wallet by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2731
- Remove put gossip by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2790
- [vms/platformvm] Remove `GetPendingValidators` API by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2817
- [vms/platformvm] Remove `ErrFutureStakeTime` check in `VerifyTx` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2797
- Remove pre-Durango block building logic and verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2823
- Remove pre-Durango checks in BLS key verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2824
- [snow/networking] Enforce `PreferredIDAtHeight` in `Chits` messages by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2827
- Combine AppGossip and AppGossipSpecific by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2836
- [network/peer] Disconnect from peers who only send legacy version field by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2830
- [vms/avm] Cleanup `GetTx` + remove state pruning logic by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2826
- [vms/avm] Remove `snow.Context` from `Network` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2834
- [vms/platformvm] Remove state pruning logic by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2825
- Prevent zero length values in slices and maps in codec by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2819
- [utils/compression] Remove gzip compressor by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2839
- Remove legacy p2p message handling by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2833
- Remove Durango codec check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2818
- Remove Pre-Durango TLS certificate parsing logic by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2831
- Remove engine type handling for everything other than GetAncestors by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2800
- P-chain:  Improve GetValidatorsSet error expressivity by @abi87 in https://github.com/ava-labs/avalanchego/pull/2808
- Add antithesis PoC workload by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2796
- Add Antithesis docker compose file by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2838
- merkledb metric naming nits by @danlaine in https://github.com/ava-labs/avalanchego/pull/2844
- Allow configuring push gossip to send txs to validators by stake by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2835
- update merkledb readme to specify key length is in bits by @danlaine in https://github.com/ava-labs/avalanchego/pull/2840
- `tmpnet`: Add a UUID to temporary networks to support metrics collection by @marun in https://github.com/ava-labs/avalanchego/pull/2763
- packer build by @Dirrk in https://github.com/ava-labs/avalanchego/pull/2806
- Bump google.golang.org/protobuf from 1.32.0 to 1.33.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2849
- Bump bufbuild/buf-setup-action from 1.29.0 to 1.30.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2842
- Remove verify height index by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2634
- Dynamic Fees - Add E Upgrade boilerplate by @abi87 in https://github.com/ava-labs/avalanchego/pull/2597
- `tmpnet`: Enable collection of logs and metrics by @marun in https://github.com/ava-labs/avalanchego/pull/2820
- P-Chain - repackaged wallet backends by @abi87 in https://github.com/ava-labs/avalanchego/pull/2757
- X-Chain - repackaged wallet backends by @abi87 in https://github.com/ava-labs/avalanchego/pull/2762
- Remove fallback validator height indexing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2801
- `tmpnet`: Reuse dynamically-allocated API port across restarts by @marun in https://github.com/ava-labs/avalanchego/pull/2857
- Remove useless bootstrapping metric by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2858
- Remove duplicate log by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2860

### New Contributors

- @Nuttymoon made their first contribution in https://github.com/ava-labs/avalanchego/pull/2794
- @Dirrk made their first contribution in https://github.com/ava-labs/avalanchego/pull/2806

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.2...v1.11.3

## [v1.11.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.2)

This version is backwards compatible to [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0). It is optional, but strongly encouraged.

The plugin version is updated to `34` all plugins must update to be compatible.

### APIs

- Removed the `ipc` API
- Removed the `auth` API
- Removed most `keystore` related methods from the `platform` API
  - `platform.importKey`
  - `platform.createAddress`
  - `platform.addValidator`
  - `platform.addDelegator`
  - `platform.addSubnetValidator`
  - `platform.createSubnet`
  - `platform.exportAVAX`
  - `platform.importAVAX`
  - `platform.createBlockchain`
- Added push gossip metrics:
  - `gossip_tracking{type="sent"}`
  - `gossip_tracking{type="unsent"}`
  - `gossip_tracking_lifetime_average`
  to the following namespaces:
  - `avalanche_P_vm_tx`
  - `avalanche_X_vm_avalanche_tx`
  - `avalanche_C_vm_sdk_atomic_tx_gossip`
  - `avalanche_C_vm_sdk_eth_tx_gossip`
- Removed metrics:
  - `avalanche_C_vm_eth_gossip_atomic_sent`
  - `avalanche_C_vm_eth_gossip_eth_txs_sent`
  - `avalanche_C_vm_eth_regossip_eth_txs_queued_attempts`
  - `avalanche_C_vm_eth_regossip_eth_txs_queued_local_tx_count`
  - `avalanche_C_vm_eth_regossip_eth_txs_queued_remote_tx_count`

### Configs

- Removed:
  - `api-ipcs-enabled`
  - `ipcs-chain-ids`
  - `ipcs-path`
  - `api-auth-required`
  - `api-auth-password`
  - `api-auth-password-file`
  - `consensus-app-gossip-validator-size`
  - `consensus-app-gossip-non-validator-size`
  - `consensus-app-gossip-peer-size`
- Removed subnet configs:
  - `appGossipValidatorSize`
  - `appGossipNonValidatorSize`
  - `appGossipPeerSize`
- Added X-chain and P-chain networking configs:
  - `push-gossip-num-validators`
  - `push-gossip-num-peers`
  - `push-regossip-num-validators`
  - `push-regossip-num-peers`
  - `push-gossip-discarded-cache-size`
  - `push-gossip-max-regossip-frequency`
  - `push-gossip-frequency`
- Removed X-chain and P-chain networking configs:
  - `legacy-push-gossip-cache-size`
- Added C-chain configs:
  - `push-gossip-num-validators`
  - `push-gossip-num-peers`
  - `push-regossip-num-validators`
  - `push-regossip-num-peers`
  - `push-gossip-frequency`
  - `pull-gossip-frequency`
  - `tx-pool-lifetime`
- Removed C-chain configs:
  - `tx-pool-journal`
  - `tx-pool-rejournal`
  - `remote-gossip-only-enabled`
  - `regossip-max-txs`
  - `remote-tx-gossip-only-enabled`
  - `tx-regossip-max-size`

### Fixes

- Fixed mempool push gossip amplification

### What's Changed

- Remove deprecated IPC API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2760
- `vms/platformvm`: Remove all keystore APIs except `ExportKey` and `ListAddresses` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2761
- Remove Deprecated Auth API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2759
- Remove `defaultAddress` helper from platformvm service tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2767
- [trace] upgrade opentelemetry to v1.22.0 by @bianyuanop in https://github.com/ava-labs/avalanchego/pull/2702
- Reenable the upgrade tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2769
- [network/p2p] Redesign Push Gossip by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/2772
- Move AppGossip configs from SubnetConfig into ChainConfig by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2785
- `merkledb` -- move compressedKey declaration to avoid usage of stale values in loop by @danlaine in https://github.com/ava-labs/avalanchego/pull/2777
- `merkledb` -- fix `hasValue` in `recordNodeDeleted` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2779
- `merkledb` -- rename metrics and add missing call by @danlaine in https://github.com/ava-labs/avalanchego/pull/2781
- `merkledb` -- style nit, remove var name `newView` to reduce shadowing by @danlaine in https://github.com/ava-labs/avalanchego/pull/2784
- `merkledb` style nits by @danlaine in https://github.com/ava-labs/avalanchego/pull/2783
- `merkledb` comment accuracy fixes by @danlaine in https://github.com/ava-labs/avalanchego/pull/2780
- Increase gossip size on first push by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2787

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.11.0...v1.11.2

## [v1.11.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.11.0)

This upgrade consists of the following Avalanche Community Proposals (ACPs):

- [ACP-23](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/23-p-chain-native-transfers/README.md) P-Chain Native Transfers
- [ACP-24](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/24-shanghai-eips/README.md) Activate Shanghai EIPs on C-Chain
- [ACP-25](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/25-vm-application-errors/README.md) Virtual Machine Application Errors
- [ACP-30](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/30-avalanche-warp-x-evm/README.md) Integrate Avalanche Warp Messaging into the EVM
- [ACP-31](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/31-enable-subnet-ownership-transfer/README.md) Enable Subnet Ownership Transfer
- [ACP-41](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/41-remove-pending-stakers/README.md) Remove Pending Stakers
- [ACP-62](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/62-disable-addvalidatortx-and-adddelegatortx/README.md) Disable AddValidatorTx and AddDelegatorTx

The changes in the upgrade go into effect at 11 AM ET (4 PM UTC) on Wednesday, March 6th, 2024 on Mainnet.

**All Durango supporting Mainnet nodes should upgrade before 11 AM ET, March 6th 2024.**

The plugin version is updated to `33` all plugins must update to be compatible.

### APIs

- Added `platform.getSubnet` API

### Configs

- Deprecated:
    - `api-auth-required`
    - `api-auth-password`
    - `api-auth-password-file`

### Fixes

- Fixed potential deadlock during P-chain shutdown
- Updated the consensus engine to recover from previously misconfigured subnets without requiring a restart

### What's Changed

- `ci`: Upgrade all workflow actions to versions using Node 20 by @marun in https://github.com/ava-labs/avalanchego/pull/2677
- `tmpnet`: Ensure restart after chain creation by @marun in https://github.com/ava-labs/avalanchego/pull/2675
- Publish docker images with race detection by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2680
- `vms/platformvm`: Remove `NewRewardValidatorTx` from `Builder` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2676
- `ci`: Updated shellcheck script to support autofix by @marun in https://github.com/ava-labs/avalanchego/pull/2678
- Unblock misconfigured subnets by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2679
- Add transfer subnet ownership functionality to wallet by @felipemadero in https://github.com/ava-labs/avalanchego/pull/2659
- Add ACP-62 by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2681
- `vms/platformvm`: Add missing txs to `txs.Builder` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2663
- `vms/platformvm`: Disable `AddValidatorTx` and `AddDelegatorTx` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2662
- Remove chain router from node.Config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2683
- Deprecate the auth API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2684
- Fix P-chain Shutdown deadlock by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2686
- Cleanup ID initialization by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2690
- Remove unused chains#beacons field by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2692
- x/sync: Remove duplicated call to TrackBandwidth by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2694
- Move VMAliaser into node from config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2689
- Fix minor errors in x/sync tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2709
- Update minimum golang version to v1.21.7 by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2710
- Check for github action updates in dependabot by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2715
- Update `golangci-lint` to `v1.56.1` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2714
- Add stringer to warp types by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2712
- Refactor `p2p.PeerTracker` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2701
- Bump actions/stale from 8 to 9 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2719
- Bump github/codeql-action from 2 to 3 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2720
- Bump bufbuild/buf-setup-action from 1.26.1 to 1.29.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2721
- Bump aws-actions/configure-aws-credentials from 1 to 4 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2722
- Manually setup golang in codeql action by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2725
- Provide pgo file during compilation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2724
- P-chain - Tx builder cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/2718
- Refactor chain manager subnets by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2711
- Replace snowball/snowflake interface with single shared snow interface by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2717
- Remove duplicate IP length constant by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2733
- Add `platform.getSubnet` API by @felipemadero in https://github.com/ava-labs/avalanchego/pull/2704
- Provide BLS signature in Handshake message by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2730
- Verify BLS signature provided in Handshake messages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2735
- Move UTXOs definition from primary to primary/common by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2741
- Minimize Signer interface and document Sign by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2740
- Revert setup-go during unit tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2744
- P-chain wallet fees UTs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2734
- `merkledb` -- generalize error case to check state that should never occur by @danlaine in https://github.com/ava-labs/avalanchego/pull/2743
- Revert setup-go to v3 on all arm actions by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2749
- Add AppError to Sender interface by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2737
- P-chain - Cleaned up fork switch in UTs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2746
- X-chain wallet fees UTs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2747
- Add keys values to bimap by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2754
- fix test sender by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2755

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.19...v1.11.0

## [v1.10.19](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.19)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `31` and is compatible with version `v1.10.18`.

### APIs

- Added `admin.dbGet` call to the `admin` API
- Added bloom filter metrics:
  - `bloom_filter_count`
  - `bloom_filter_entries`
  - `bloom_filter_hashes`
  - `bloom_filter_max_count`
  - `bloom_filter_reset_count`
  to the following namespaces:
  - `avalanche_X_vm_mempool`
  - `avalanche_P_vm_mempool`
  - `avalanche_C_vm_sdk_atomic_mempool`
  - `avalanche_C_vm_sdk_eth_mempool`

### Fixes

- Fixed race condition during validator set creation
- Fixed C-chain mempool bloom filter recalculation

### What's Changed

- `vms/platformvm`: Change `AdvanceTimeTo` to modify passed-in `parentState` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2489
- `vms/platformvm`: Remove `MempoolTxVerifier` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2362
- Verify `SignedIP.Timestamp` from `PeerList` messages by @danlaine in https://github.com/ava-labs/avalanchego/pull/2587
- Fix metrics namespace by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2632
- Add bloom filter metrics to the p2p sdk by @ceyonur in https://github.com/ava-labs/avalanchego/pull/2612
- Replace `shutdownEnvironment` with `t.Cleanup()` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2491
- P-chain - Memo field zeroed post Durango  by @abi87 in https://github.com/ava-labs/avalanchego/pull/2607
- Refactor feature extensions out of VMManager by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2578
- Remove getter for router on chain manager by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2641
- Fix `require.ErrorIs` argument order by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2645
- `api/admin`: Cleanup `SuccessResponseTests` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2644
- Allow calls to `Options` before `Verify` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2363
- Improve logging of unexpected proposer errors by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2646
- Disable non-security related dependabot PRs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2647
- Add historical fork times by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2649
- Cleanup warp signer tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2651
- Reintroduce the upgrade test against v1.10.18 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2652
- Cleanup database benchmarks by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2653
- Cleanup database tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2654
- `ci`: Add shellcheck step to lint job by @marun in https://github.com/ava-labs/avalanchego/pull/2650
- Replace `closeFn` with `t.Cleanup` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2638
- Fix TestExpiredBuildBlock by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2655
- Add admin.dbGet API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2667
- `ci`: Update shellcheck.sh to pass all args to shellcheck by @marun in https://github.com/ava-labs/avalanchego/pull/2657
- `vms/platformvm`: Remove `NewAdvanceTimeTx` from `Builder` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2668
- Log error if database returns unsorted heights by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2670
- `vms/platformvm`: Move `vm.Shutdown` call in tests to `t.Cleanup` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2669
- `e2e`: Add test of `platform.getValidatorsAt` across nodes by @marun in https://github.com/ava-labs/avalanchego/pull/2664
- Fix P-chain validator set lookup race condition by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2672

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.18...v1.10.19

## [v1.10.18](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.18)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is updated to `31` all plugins must update to be compatible.

### APIs

- Added `info.acps` API
- Added `supportedACPs` and `objectedACPs` for each peer returned by `info.peers`
- Added `txs` field to `BanffProposalBlock`'s json format
- Added metrics:
  - `avalanche_network_validator_ips`
  - `avalanche_network_gossipable_ips`
  - `avalanche_network_ip_bloom_count`
  - `avalanche_network_ip_bloom_entries`
  - `avalanche_network_ip_bloom_hashes`
  - `avalanche_network_ip_bloom_max_count`
  - `avalanche_network_ip_bloom_reset_count`
- Added metrics related to `get_peer_list` message handling
- Added p2p SDK metrics to  the P-chain and X-chain
- Renamed metrics related to message handling:
  - `version` -> `handshake`
  - `appRequestFailed` -> `appError`
  - `crossChainAppRequestFailed` -> `crossChainAppError`
- Removed `gzip` compression time metrics
- Converted p2p SDK metrics to use vectors rather than independent metrics
- Converted client name reported over the p2p network from `avalanche` to `avalanchego`

### Configs

- Added:
  - `--acp-support`
  - `--acp-object`
  - `snow-commit-threshold`
  - `network-peer-list-pull-gossip-frequency`
  - `network-peer-list-bloom-reset-frequency`
  - `network` to the X-chain and P-chain configs including:
    - `max-validator-set-staleness`
    - `target-gossip-size`
    - `pull-gossip-poll-size`
    - `pull-gossip-frequency`
    - `pull-gossip-throttling-period`
    - `pull-gossip-throttling-limit`
    - `expected-bloom-filter-elements`
    - `expected-bloom-filter-false-positive-probability`
    - `max-bloom-filter-false-positive-probability`
    - `legacy-push-gossip-cache-size`
- Deprecated:
    - `snow-virtuous-commit-threshold`
    - `snow-rogue-commit-threshold`
    - `network-peer-list-validator-gossip-size`
    - `network-peer-list-non-validator-gossip-size`
    - `network-peer-list-peers-gossip-size`
    - `network-peer-list-gossip-frequency`
- Removed:
  - `gzip` as an option for `network-compression-type`

### Fixes

- Fixed `platformvm.SetPreference` to correctly reset the block building timer
- Fixed early bootstrapping termination
- Fixed duplicated transaction initialization in the X-chain and P-chain
- Fixed IP gossip when using dynamically allocated staking ports
- Updated `golang.org/x/exp` dependency to fix downstream compilation errors
- Updated `golang.org/x/crypto` dependency to address `CVE-2023-48795`
- Updated minimum golang version to address `CVE-2023-39326`
- Restricted `GOPROXY` during compilation to avoid `direct` version control fallbacks
- Fixed `merkledb` deletion of the empty key
- Fixed `merkledb` race condition when interacting with invalidated or closed trie views
- Fixed `json.Marshal` for `wallet` transactions
- Fixed duplicate outbound dialer for manually tracked nodes in the p2p network

### What's Changed

- testing: Update to latest version of ginkgo by @marun in https://github.com/ava-labs/avalanchego/pull/2390
- `vms/platformvm`: Cleanup block builder tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2406
- Drop Pending Stakers 0 - De-duplicate staking tx verification by @abi87 in https://github.com/ava-labs/avalanchego/pull/2335
- `vms/platformvm`: Initialize txs in `Transactions` field for `BanffProposalBlock` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2419
- `vms/platformvm`: Move `VerifyUniqueInputs` from `verifier` to `backend` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2410
- Fix duplicated bootstrapper engine termination by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2334
- allow user of `build_fuzz.sh` to specify a directory to fuzz in by @danlaine in https://github.com/ava-labs/avalanchego/pull/2414
- Update slices dependency to use Compare by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2424
- `vms/platformvm`: Cleanup some block tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2422
- ProposerVM Extend windows 0 - Cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/2404
- `vms/platformvm`: Add `decisionTxs` parameter to `NewBanffProposalBlock` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2411
- Update minimum golang version to v1.20.12 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2427
- Fix platformvm.SetPreference by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2429
- Restrict GOPROXY by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2434
- Drop Pending Stakers 1 - introduced ScheduledStaker txs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2323
- Run merkledb fuzz tests every 6 hours by @danlaine in https://github.com/ava-labs/avalanchego/pull/2415
- Remove unused error by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2426
- Make `messageQueue.msgAndCtxs` a circular buffer by @danlaine in https://github.com/ava-labs/avalanchego/pull/2433
- ProposerVM Extend windows 1 - UTs Cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/2412
- Change seed from int64 to uint64 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2438
- Remove usage of timer.Timer in node by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2441
- Remove staged timer again by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2440
- `merkledb` / `sync` -- Disambiguate no end root from no start root by @danlaine in https://github.com/ava-labs/avalanchego/pull/2437
- Drop Pending Stakers 2 - Replace txs.ScheduledStaker with txs.Staker by @abi87 in https://github.com/ava-labs/avalanchego/pull/2305
- `vms/platformvm`: Remove double block building logic by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2380
- Remove usage of timer.Timer in benchlist by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2446
- `vms/avm`: Simplify `Peek` function in mempool by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2449
- `vms/platformvm`: Remove `standardBlockState` struct by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2450
- Refactor sampler seeding by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2456
- Update tmpnet fixture to include Proof-of-Possession for initial stakers by @marun in https://github.com/ava-labs/avalanchego/pull/2391
- `vms/platformvm`: Remove `EnableAdding` and `DisableAdding` from `Mempool` interface by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2463
- `vms/avm`: Add `exists` bool to mempool `Peek` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2465
- `vms/platformvm`: Remove `PeekTxs` from `Mempool` interface by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2378
- `vms/platformvm`: Add `processStandardTxs` helper by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2461
- `vms/platformvm`: Process `atomicRequests` and `onAcceptFunc` in option blocks by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2459
- `e2e`: Rename 'funded key' to 'pre-funded key' for consistency by @marun in https://github.com/ava-labs/avalanchego/pull/2455
- `vms/platformvm`: Surface `VerifyUniqueInputs` in the `Manager` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2467
- `vms/platformvm`: Add `TestBuildBlockShouldReward` test by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2466
- Switch client version to a proto type from a string by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2188
- Remove stale TODO by @danlaine in https://github.com/ava-labs/avalanchego/pull/2468
- `vms/platformvm`: Add `TestBuildBlockDoesNotBuildWithEmptyMempool` test by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2469
- `vms/platformvm`: Add `TestBuildBlockShouldAdvanceTime` test by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2471
- `vms/platformvm`: Permit usage of the `Transactions` field in `BanffProposalBlock` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2451
- `vms/platformvm`: Add `TestBuildBlockForceAdvanceTime` test by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2472
- P2P AppError handling by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2248
- `vms/platformvm`: Verify txs before building a block by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2359
- Refactor p2p unit tests by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2475
- Add ACP signaling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2476
- Refactor SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2452
- Cleanup CI by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2480
- Ensure upgrade test uses the correct binary on restart by @marun in https://github.com/ava-labs/avalanchego/pull/2478
- Prefetch Improvement by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2435
- ci: run each fuzz test for 10 seconds by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2483
- Remove nullable options by @nytzuga in https://github.com/ava-labs/avalanchego/pull/2481
- `merkledb` -- dynamic root by @danlaine in https://github.com/ava-labs/avalanchego/pull/2177
- fix onEvictCache by @danlaine in https://github.com/ava-labs/avalanchego/pull/2484
- Remove cached node bytes from merkle nodes  by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2393
- Fix race in view iteration by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2486
- MerkleDB -- update readme by @danlaine in https://github.com/ava-labs/avalanchego/pull/2423
- Drop Pending Stakers 3 - persist stakers' StartTime by @abi87 in https://github.com/ava-labs/avalanchego/pull/2306
- SDK Push Gossiper implementation by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2428
- `tmpnet`: Move tmpnet/local to tmpnet package  by @marun in https://github.com/ava-labs/avalanchego/pull/2457
- `merkledb` -- make tests use time as randomness seed by @danlaine in https://github.com/ava-labs/avalanchego/pull/2470
- `tmpnet`: Break config.go up into coherent parts by @marun in https://github.com/ava-labs/avalanchego/pull/2462
- Drop Pending Stakers 4 - minimal UT infra cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/2332
- ProposerVM Extend windows 2- extend windowing by @abi87 in https://github.com/ava-labs/avalanchego/pull/2401
- Support json marshalling txs returned from the wallet by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2494
- Avoid escaping to improve readability by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2496
- Allow OutputOwners to be json marshalled without InitCtx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2495
- Drop Pending Stakers 5 - validated PostDurango StakerTxs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2314
- Bump golang.org/x/crypto from 0.14.0 to 0.17.0 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2502
- Remove unused `BuildGenesisTest` function by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2503
- Remove unused `AcceptorTracker` struct by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2508
- Dedupe secp256k1 key usage in tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2511
- Merkledb readme updates by @danlaine in https://github.com/ava-labs/avalanchego/pull/2510
- Gossip Test structs by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2514
- `tmpnet`: Separate node into orchestration, config and process  by @marun in https://github.com/ava-labs/avalanchego/pull/2460
- Move `snow.DefaultConsensusContextTest` to `snowtest.ConsensusContext` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2507
- Add gossip Marshaller interface by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2509
- Include chain creation error in health check by @marun in https://github.com/ava-labs/avalanchego/pull/2519
- Make X-chain mempool safe for concurrent use by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2520
- Initialize transactions once by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2521
- `vms/avm`: Remove usage of `require.Contains` from service tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2517
- Move context lock into issueTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2524
- Rework X-chain locking in tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2526
- `vms/avm`: Simplify `mempool.Remove` signature by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2527
- Remove unused mocks by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2528
- Move `avm.newContext` to `snowtest.Context` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2513
- Do not fail-fast Tests / Unit by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2530
- Make P-Chain Mempool thread-safe by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2523
- `vms/platformvm`: Use `snowtest.Context` helper by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2515
- Export mempool errors by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2531
- Move locking into issueTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2532
- Fix merge in wallet service by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2534
- Introduce TxVerifier interface to network by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2533
- Export P-Chain Mempool Errors by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2535
- Rename `Version` message to  `Handshake` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2479
- Rename myVersionTime to ipSigningTime by @danlaine in https://github.com/ava-labs/avalanchego/pull/2537
- Remove resolved TODO by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2540
- Only initialize Txs once by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2538
- JSON marshal the `Transactions` field in `BanffProposalBlocks` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2541
- Enable `predeclared` linter by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2539
- Move context lock into `network.issueTx` by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2525
- Remove comment on treating failed sends as FATAL by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2544
- Add TxVerifier interface to network by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2542
- X-chain SDK gossip by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2490
- Remove network context by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2543
- Remove `snow.DefaultContextTest` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2518
- Fix windowing when no validator is available by @abi87 in https://github.com/ava-labs/avalanchego/pull/2529
- Unexport fields from gossip.BloomFilter by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2547
- P-Chain SDK Gossip by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2487
- Documentation Fixes: Grammatical Corrections and Typo Fixes Across Multiple Files by @joaolago1113 in https://github.com/ava-labs/avalanchego/pull/2550
- Notify block builder of txs after reject by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2549
- Set dependabot target branch to `dev` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2553
- Remove `MockLogger` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2554
- Clean up merkleDB interface and duplicate code by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2445
- Do not mark txs as dropped when mempool is full by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2557
- Update bug bounty program to immunefi by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2558
- Fix p2p sdk metric labels by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2561
- Suppress gossip warnings due to no sampled peers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2562
- Remove dead code and unnecessary lock from reflect codec by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2560
- Remove unused index interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2564
- Implement SetMap and use it in XP-chain mempools by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2555
- `vms/platformvm`: Add `TestIterate` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2565
- Cleanup codec usage by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2563
- Remove `len` tag parsing from the reflect codec by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2559
- Use more specific type by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2567
- Standardize `onShutdownCtx` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2568
- Verify avm mempool txs against the last accepted state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2569
- Update `CODEOWNERS` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2570
- Remove license from mocks by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2574
- Add missing import by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2573
- `vms/platformvm`: Prune mempool periodically by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2566
- Update license header to 2024 by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2572
- [MerkleDB] Make intermediate node cache two layered by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2576
- Fix merkledb rebuild iterator by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2581
- Fix intermediate node caching by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2585
- Remove codec length check after Durango by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2586
- `tmpnet`: Use AvalancheLocalChainConfig for cchain genesis by @marun in https://github.com/ava-labs/avalanchego/pull/2583
- `testing`: Ensure CheckBootstrapIsPossible is safe for teardown by @marun in https://github.com/ava-labs/avalanchego/pull/2582
- `tmpnet`: Separate network into orchestration and configuration by @marun in https://github.com/ava-labs/avalanchego/pull/2464
- Update uintsize implementation by @danlaine in https://github.com/ava-labs/avalanchego/pull/2590
- Optimize bloom filter by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2588
- Remove TLS key gen from networking tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2596
- [utils/bloom] Optionally Update Bloom Filter Size on Reset by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/2591
- [ci] Increase Fuzz Time in Periodic Runs by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/2599
- `tmpnet`: Save metrics snapshot to disk before node shutdown by @marun in https://github.com/ava-labs/avalanchego/pull/2601
- chore: Fix typo s/useage/usage by @hugo-syn in https://github.com/ava-labs/avalanchego/pull/2602
- Deprecate `SnowRogueCommitThresholdKey` and `SnowVirtuousCommitThresholdKey` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2600
- Fix networking invalid field log by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2604
- chore: Fix typo s/seperate/separate/ by @hugo-syn in https://github.com/ava-labs/avalanchego/pull/2605
- Support dynamic port peerlist gossip by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2603
- Replace `PeerListAck` with `GetPeerList` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2580
- Log critical consensus values during health checks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2609
- Update contributions branch to master by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2610
- Add ip bloom metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2614
- `x/sync`: Auto-generate `MockNetworkClient` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2617
- Remove CreateStaticHandlers from VM interface by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2589
- `tmpnet`: Add support for subnets by @marun in https://github.com/ava-labs/avalanchego/pull/2492
- Update `go.uber.org/mock/gomock` to `v0.4.0` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2618
- Add `mockgen` source mode for generics + bls imports by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2615
- Verify all MockGen generated files are re-generated in CI by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2616
- Move division by 0 check out of the bloom loops by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2622
- P-chain Add UTs around stakers persistence in platformvm state by @abi87 in https://github.com/ava-labs/avalanchego/pull/2505
- Revert "Set dependabot target branch to `dev` (#2553)" by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2623
- Remove remaining 2023 remnants by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2624
- Deprecate push-based peerlist gossip flags by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2625
- Remove support for compressing gzip messages by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2627
- Always attempt to install mockgen `v0.4.0` before execution by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2628
- Modify TLS parsing rules for Durango by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2458

### New Contributors

- @joaolago1113 made their first contribution in https://github.com/ava-labs/avalanchego/pull/2550
- @hugo-syn made their first contribution in https://github.com/ava-labs/avalanchego/pull/2602

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.17...v1.10.18

## [v1.10.17](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.17)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `30` and is compatible with versions `v1.10.15-v1.10.16`.

### APIs

- Added `avalanche_{chainID}_blks_build_accept_latency` metric
- Added `avalanche_{chainID}_blks_issued{source}` metric with sources:
  -  `pull_gossip`
  -  `push_gossip`
  -  `put_gossip` which is deprecated
  -  `built`
  -  `unknown`
- Added `avalanche_{chainID}_issuer_stake_sum` metric
- Added `avalanche_{chainID}_issuer_stake_count` metric

### Configs

- Added:
  - `--consensus-frontier-poll-frequency`
- Removed:
  - `--consensus-accepted-frontier-gossip-frequency`
- Deprecated:
  - `--consensus-accepted-frontier-gossip-validator-size`
  - `--consensus-accepted-frontier-gossip-non-validator-size`
  - `--consensus-accepted-frontier-gossip-peer-size`
    - Updated the default value to 1 to align with the change in default gossip frequency
  - `--consensus-on-accept-gossip-validator-size`
  - `--consensus-on-accept-gossip-non-validator-size`
  - `--consensus-on-accept-gossip-peer-size`

### Fixes

- Fixed `duplicated operation on provided value` error when executing atomic operations after state syncing the C-chain
- Removed usage of atomic trie after commitment
- Fixed atomic trie root overwrite during state sync
- Prevented closure of `stdout` and `stderr` when shutting down the logger

### What's Changed

- Remove Banff check from mempool verifier by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2360
- Document storage growth in readme by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2364
- Add metric for duration between block timestamp and acceptance time by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2366
- `vms/platformvm`: Remove unused `withMetrics` txheap by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2373
- Move peerTracker from x/sync to network/p2p by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2356
- Logging avoid closing standard outputs by @felipemadero in https://github.com/ava-labs/avalanchego/pull/2372
- `vms/platformvm`: Adjust `Diff.Apply` signature by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2368
- Add bls validator info to genesis by @felipemadero in https://github.com/ava-labs/avalanchego/pull/2371
- Remove `engine.GetVM` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2374
- `vms/platformvm`: Consolidate `state` pkg mocks by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2370
- Remove common bootstrapper by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2297
- `vms/platformvm`: Move `toEngine` channel to mempool by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2333
- `vms/avm`: Rename `states` pkg to `state` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2381
- Implement generic bimap by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2383
- Unexport RequestID from snowman engine by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2384
- Add metric to track the stake weight of block providers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2376
- Add block source metrics to monitor gossip by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2386
- Rename `D` to `Durango` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2389
- Replace periodic push accepted gossip with pull preference gossip for block discovery by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2367
- MerkleDB Remove ID from Node to reduce size and removal channel creation. by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2324
- Remove method `CappedList` from `set.Set` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2395
- Periodically PullGossip only from connected validators by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2399
- Update bootstrap IPs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2396
- Rename `testnet` fixture to `tmpnet` by @marun in https://github.com/ava-labs/avalanchego/pull/2307
- Add `p2p.Network` component by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2283
- `vms/platformvm`: Move `GetRewardUTXOs`, `GetSubnets`, and `GetChains` to `State` interface by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2402
- Add more descriptive formatted error by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2403

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.16...v1.10.17

## [v1.10.16](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.16)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `30` and compatible with version `v1.10.15`.

### APIs

- Added log level information to the result of `admin.setLoggerLevel`
- Updated `info.peers` to return chain aliases for `benched` chains
- Added support to sample validators of non-tracked subnets with `platform.sampleValidators`
- Added `avalanche_{chainID}_max_verified_height` metric to track the highest verified block

### Configs

- Added `--db-read-only` to run the node without writing to disk.
  - This flag is only expected to be used during testing as it will cause memory use to increase over time
- Removed `--bootstrap-retry-enabled`
- Removed `--bootstrap-retry-warn-frequency`

### Fixes

- Fixed packing of large block requests during C-chain state sync
- Fixed order of updating acceptor tip and sending chain events to C-chain event subscribers

### What's Changed

- Return log levels from admin.SetLoggerLevel by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2250
- feat(api) : Peers function to return the PrimaryAlias of the chainID by @DoTheBestToGetTheBest in https://github.com/ava-labs/avalanchego/pull/2251
- Switch to using require.TestingT interface in SenderTest struct by @marun in https://github.com/ava-labs/avalanchego/pull/2258
- Cleanup `ipcs` `Socket` test by @danlaine in https://github.com/ava-labs/avalanchego/pull/2257
- Require poll metrics to be registered by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2260
- Track all subnet validator sets in the validator manager by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2253
- e2e: Make NewWallet and NewEthclient regular functions by @marun in https://github.com/ava-labs/avalanchego/pull/2262
- Fix typos in docs by @vuittont60 in https://github.com/ava-labs/avalanchego/pull/2261
- Remove Token constants information from keys by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2197
- Remove unused `UnsortedEquals` function by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2264
- Document p2p package by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2254
- Use extended public key to derive ledger addresses by @felipemadero in https://github.com/ava-labs/avalanchego/pull/2246
- `merkledb` -- rename nit by @danlaine in https://github.com/ava-labs/avalanchego/pull/2267
- `merkledb` -- fix nil check in test by @danlaine in https://github.com/ava-labs/avalanchego/pull/2268
- Add read-only database flag (`--db-read-only`)  by @danlaine in https://github.com/ava-labs/avalanchego/pull/2266
- `merkledb` -- remove unneeded var declarations by @danlaine in https://github.com/ava-labs/avalanchego/pull/2269
- Add fuzz test for `NewIteratorWithStartAndPrefix` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1992
- Return if element was deleted from `Hashmap` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2271
- `mempool.NewMempool` -> `mempool.New` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2276
- e2e: Refactor suite setup and helpers to tests/fixture/e2e for reuse by coreth by @marun in https://github.com/ava-labs/avalanchego/pull/2265
- Cleanup platformvm mempool errs by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2278
- MerkleDB:Naming and comments cleanup by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2274
- Move `DropExpiredStakerTxs` to platformvm mempool by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2279
- Cleanup `ids.NodeID` usage by @abi87 in https://github.com/ava-labs/avalanchego/pull/2280
- Genesis validators cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/2282
- Remove Lazy Initialize on Node by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1384
- Remove sentinel node from MerkleDB proofs by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2106
- Embed `noop` handler for all unhandled messages by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2288
- `merkledb` -- Add `Clearer` interface  by @danlaine in https://github.com/ava-labs/avalanchego/pull/2277
- Simplify get server creation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2285
- Move management of platformvm preferred block to `executor.Manager` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2292
- Add `recentTxsLock` to platform `network` struct by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2294
- e2e: More fixture refinement in support of coreth integration testing  by @marun in https://github.com/ava-labs/avalanchego/pull/2275
- Add `VerifyTx` to `executor.Manager` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2293
- Simplify avalanche bootstrapping by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2286
- Replace unique slices with sets in the engine interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2317
- Use zap.Stringer rather than zap.Any by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2320
- Move `AddUnverifiedTx` logic to `network.IssueTx` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2310
- Remove `AddUnverifiedTx` from `Builder` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2311
- Remove error from SDK AppGossip handler by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2252
- Rename AppRequestFailed to AppError by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2321
- Remove `Network` interface from `Builder` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2312
- Update `error_code` to be sint32 instead of uint32. by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2322
- Refactor bootstrapper implementation into consensus by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2300
- Pchain - Cleanup NodeID generation in UTs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2291
- nit: loop --> variadic by @danlaine in https://github.com/ava-labs/avalanchego/pull/2316
- Update zap dependency to v1.26.0 by @danlaine in https://github.com/ava-labs/avalanchego/pull/2325
- Remove useless anon functions by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2326
- Move `network` implementation to separate package by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2296
- Unexport avalanche constant from common package by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2327
- Remove `common.Config` functions by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2328
- Move engine startup into helper function by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2329
- Remove bootstrapping retry config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2301
- Export snowman bootstrapper by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2331
- Remove common.Config from syncer.Config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2330
- `platformvm.VM` -- replace `Config` field with `validators.Manager` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2319
- Improve height monitoring by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2347
- Cleanup snowman consensus metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2349
- Expand consensus health check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2354
- Reduce the size of the OracleBlock interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2355
- [vms/proposervm] Update Build Heuristic by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/2348
- Use linkedhashmap for P-Chain mempool by @gyuho in https://github.com/ava-labs/avalanchego/pull/1536
- Increase txs in pool metric when adding tx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2361

### New Contributors

- @DoTheBestToGetTheBest made their first contribution in https://github.com/ava-labs/avalanchego/pull/2251
- @vuittont60 made their first contribution in https://github.com/ava-labs/avalanchego/pull/2261

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.15...v1.10.16

## [v1.10.15](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.15)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is updated to `30` all plugins must update to be compatible.

### Configs

- Added `pebble` as an allowed option to `--db-type`

### Fixes

- Fixed C-chain tracer API panic

### What's Changed

- Reduce allocations on insert and remove by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2201
- `merkledb` -- shift nit by @danlaine in https://github.com/ava-labs/avalanchego/pull/2218
- Update `golangci-lint` to `v1.55.1` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2228
- Add json marshal tests to existing serialization tests in `platformvm/txs` pkg by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2227
- Move all blst function usage to `bls` pkg by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2222
- `merkledb` -- don't pass `BranchFactor` to `encodeDBNode` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2217
- Add `utils.Err` helper by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2212
- Enable `perfsprint` linter by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2229
- Trim down size of secp256k1 `Factory` struct by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2223
- Fix test typos by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2233
- P2P AppRequestFailed protobuf definition by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2111
- Remove error from Router AppGossip by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2238
- Document host and port behavior in help text by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2236
- Remove `database.Manager` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2239
- Add `BaseTx` support to platformvm by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2232
- Add `pebble` as valid value for `--db-type`. by @danlaine in https://github.com/ava-labs/avalanchego/pull/2244
- Add nullable option to codec by @nytzuga in https://github.com/ava-labs/avalanchego/pull/2171

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.14...v1.10.15

## [v1.10.14](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.14)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `29` and compatible with version `v1.10.13`.

### Configs

- Deprecated `--api-ipcs-enabled`
- Deprecated `--ipcs-chain-ids`
- Deprecated `--ipcs-path`
- Deprecated `--api-keystore-enabled`

### Fixes

- Fixed shutdown of timeout manager
- Fixed racy access of the shutdown time

### What's Changed

- Remove build check from unit tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2189
- Update cgo usage by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2184
- Deprecate IPC configs by @danlaine in https://github.com/ava-labs/avalanchego/pull/2168
- Update P2P proto docs by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2181
- Merkle db Make Paths only refer to lists of nodes by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2143
- Deprecate keystore config by @danlaine in https://github.com/ava-labs/avalanchego/pull/2195
- Add tests for BanffBlock serialization by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2194
- Move Shutdown lock from Handler into Engines by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2179
- Move HealthCheck lock from Handler into Engines by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2173
- Implement Heap Map by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2137
- Move selectStartGear lock from Handler into Engines by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2182
- Add Heap Set by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2136
- Shutdown TimeoutManager during node Shutdown by @abi87 in https://github.com/ava-labs/avalanchego/pull/1707
- Redesign validator set management to enable tracking all subnets by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1857
- Update local network readme by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2203
- Use custom codec for validator metadata by @abi87 in https://github.com/ava-labs/avalanchego/pull/1510
- Add RSA max key length test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2205
- Remove duplicate networking check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2204
- Update TestDialContext to use ManuallyTrack by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2209
- Remove contains from validator manager interface by @ceyonur in https://github.com/ava-labs/avalanchego/pull/2198
- Move the overridden manager into the node by @ceyonur in https://github.com/ava-labs/avalanchego/pull/2199
- Remove `aggregate` struct by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2213
- Add log for ungraceful shutdown on startup by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2215
- Add pebble database implementation by @danlaine in https://github.com/ava-labs/avalanchego/pull/1999
- Add `TransferSubnetOwnershipTx` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2178
- Revert networking AllowConnection change by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2219
- Fix unexpected unlock by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2221
- Improve logging for block verification failure by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2224

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.13...v1.10.14

## [v1.10.13](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.13)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is updated to `29` all plugins must update to be compatible.

### Fixes

- Added `Prefetcher` to the `merkledb` interface
- Fixed json marshalling of `TrackedSubnets` and `AllowedNodes`

### What's Changed

- Fix typo in block formation logic documentation by @kyoshisuki in https://github.com/ava-labs/avalanchego/pull/2158
- Marshal blocks and transactions inside API calls by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2153
- Remove lock options from the info api by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2149
- Remove write lock option from the avm static API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2154
- Remove write lock option from the avm wallet API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2155
- Fix json marshalling of Sets by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2161
- Rename `removeSubnetValidatorValidation` to `verifyRemoveSubnetValidatorTx` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2162
- Remove lock options from the IPCs api by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2151
- Remove write lock option from the xsvm API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2152
- Remove lock options from the admin API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2150
- Remove aliasing of `math` standard lib by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2163
- Remove write lock option from the platformvm API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2157
- Remove write lock option from the avm rpc API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2156
- Remove context lock from API VM interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2165
- Use set.Of rather than set.Add by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2164
- Bump google.golang.org/grpc from 1.55.0 to 1.58.3 by @dependabot in https://github.com/ava-labs/avalanchego/pull/2159
- [x/merkledb] `Prefetcher` interface by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/2167
- Validator Diffs: docs and UTs cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/2037
- MerkleDB Reduce buffer creation/memcopy on path construction by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2124
- Fix some P-chain UTs by @abi87 in https://github.com/ava-labs/avalanchego/pull/2117

### New Contributors

- @kyoshisuki made their first contribution in https://github.com/ava-labs/avalanchego/pull/2158

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.12...v1.10.13

## [v1.10.12](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.12)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `28` and compatible with versions `v1.10.9 - v1.10.11`.

### APIs

- Added `avalanche_{chainID}_total_weight` metric
- Added `avalanche_{chainID}_num_validators` metric
- Added `avalanche_{chainID}_num_processing_ancestor_fetches_failed` metric
- Added `avalanche_{chainID}_num_processing_ancestor_fetches_dropped` metric
- Added `avalanche_{chainID}_num_processing_ancestor_fetches_succeeded` metric
- Added `avalanche_{chainID}_num_processing_ancestor_fetches_unneeded` metric
- Added `avalanche_{chainID}_num_missing_accepted_blocks` metric
- Added `avalanche_{chainID}_selected_vote_index_count` metric
- Added `avalanche_{chainID}_selected_vote_index_sum` metric

### Configs

- Added `--snow-preference-quorum-size` flag
- Added `--snow-confidence-quorum-size` flag
- Added `"fx-owner-cache-size"` to the P-chain config

### Fixes

- Fixed concurrent node shutdown and chain creation race
- Updated http2 implementation to patch CVE-2023-39325
- Exited `network.dial` early to avoid goroutine leak when shutting down
- Reduced log level of `"failed to send peer list for handshake"` messages from `ERROR` to `DEBUG`
- Reduced log level of `"state pruning failed"` messages from `ERROR` to `WARN`

### What's Changed

- Add last accepted height to the snowman interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2091
- Delete kurtosis CI jobs by @marun in https://github.com/ava-labs/avalanchego/pull/2068
- e2e: Ensure all Issue* calls use the default context by @marun in https://github.com/ava-labs/avalanchego/pull/2069
- Remove Finalized from the consensus interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2093
- Remove embedding of `verify.Verifiable` in `FxCredential` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2089
- Clarify decidable interface simple default parameter tests by @gyuho in https://github.com/ava-labs/avalanchego/pull/2094
- snow/consensus/snowman/poll: remove "unused" no early term poller by @gyuho in https://github.com/ava-labs/avalanchego/pull/2095
- Cleanup `.golangci.yml` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2097
- Refactor `ancestor.Tree` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2099
- Update AMI runner image and instance type by @charlie-ava in https://github.com/ava-labs/avalanchego/pull/1939
- Add `tagalign` linter by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2084
- Fix flaky BuildBlockIsIdempotent test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2101
- Make `network.dial` honor context cancellation. by @danlaine in https://github.com/ava-labs/avalanchego/pull/2061
- Add preference lookups by height to the consensus interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2092
- Remove duplicate pullQuery method by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2103
- Add additional validator set metrics by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2051
- Remove `snowball.Initialize` and `snowball.Factory` by @danlaine in https://github.com/ava-labs/avalanchego/pull/2104
- Remove initialize functions from the snowball package by @danlaine in https://github.com/ava-labs/avalanchego/pull/2105
- Remove `genesis.State` by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2112
- add `SetSubnetOwner` to `Chain` interface by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2031
- Move vote bubbling before poll termination by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2100
- testing: Switch upgrade test to testnet fixture by @marun in https://github.com/ava-labs/avalanchego/pull/1887
- Reduce archivedb key lengths by 1 byte by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2113
- Cleanup uptime manager constructor by @abi87 in https://github.com/ava-labs/avalanchego/pull/2118
- MerkleDB Compact Path Bytes by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2010
- MerkleDB Path changes cleanup by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2120
- Fix consensus engine interface comments by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2115
- Standardize consensus variable names in tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2129
- Prevent bytesNeeded overflow by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2130
- Migrate xsvm from github.com/ava-labs/xsvm by @marun in https://github.com/ava-labs/avalanchego/pull/2045
- Fix handling of wg in the networking dial test by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2132
- Update go.mod and add update check by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2133
- Reduce log level of failing to send a peerList message by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2134
- RPCChainVM fail-fast health RPCs by @hexfusion in https://github.com/ava-labs/avalanchego/pull/2123
- MerkleDB allow warming node cache by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2128
- Add vote bubbling metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2138
- Reduce log level of an error during Prune by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2141
- Exit chain creation routine before shutting down chain router by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2140
- Merkle db fix type cast bug by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2142
- Add Warp Payload Types by @nytzuga in https://github.com/ava-labs/avalanchego/pull/2116
- Add height voting for chits by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2102
- Add Heap Queue by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2135
- Add additional payload.Hash examples by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2145
- Split Alpha into AlphaPreference and AlphaConfidence by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2125

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.11...v1.10.12

## [v1.10.11](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.11)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `28` and compatible with versions `v1.10.9 - v1.10.10`.

### Fixes

- Prevented overzelous benching due to dropped AppRequests
- Populated the process file atomically to avoid racy reads

### What's Changed

- Rename platformvm/blocks to platformvm/block by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1980
- RewardValidatorTx cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/1891
- Cancel stale SH actions by @danlaine in https://github.com/ava-labs/avalanchego/pull/2003
- e2e: Switch assertion library from gomega to testify by @marun in https://github.com/ava-labs/avalanchego/pull/1909
- e2e: Add bootstrap checks to migrated kurtosis tests by @marun in https://github.com/ava-labs/avalanchego/pull/1935
- Add `GetTransformSubnetTx` helper by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2047
- Add readme for the staking/local folder by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2046
- use `IsCortinaActivated` helper by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2048
- add `D` upgrade boilerplate by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2049
- e2e: Ensure interchain workflow coverage for the P-Chain by @marun in https://github.com/ava-labs/avalanchego/pull/1882
- e2e: Switch to using default timed context everywhere by @marun in https://github.com/ava-labs/avalanchego/pull/1910
- Remove indentation + confusing comment by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2053
- Delete ErrDelegatorSubset by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2055
- Fix default validator start time by @marun in https://github.com/ava-labs/avalanchego/pull/2058
- Enable workflows to be triggered by merge queue by @marun in https://github.com/ava-labs/avalanchego/pull/2057
- e2e: Migrate staking rewards test from kurtosis by @marun in https://github.com/ava-labs/avalanchego/pull/1767
- Fix LRU documentation comment by @anusha-ctrl in https://github.com/ava-labs/avalanchego/pull/2036
- Ignore AppResponse timeouts for benching by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2066
- trace: provide appName and version from Config by @najeal in https://github.com/ava-labs/avalanchego/pull/1893
- Update perms.WriteFile to write atomically  by @marun in https://github.com/ava-labs/avalanchego/pull/2063
- ArchiveDB by @nytzuga in https://github.com/ava-labs/avalanchego/pull/1911

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.10...v1.10.11

## [v1.10.10](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.10)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `28` and compatible with version `v1.10.9`.

### APIs

- Added `height` to the output of `platform.getCurrentSupply`

### Configs

- Added `proposerNumHistoricalBlocks` to subnet configs

### Fixes

- Fixed handling of `SIGTERM` signals in plugin processes prior to receiving a `Shutdown` message
- Fixed range proof commitment of empty proofs

### What's Changed

- e2e: Save network data for each test run as an uploaded artifact by @marun in https://github.com/ava-labs/avalanchego/pull/1856
- e2e: Ensure interchain workflow coverage for X-Chain and C-Chain by @marun in https://github.com/ava-labs/avalanchego/pull/1871
- MerkleDB Adjust New View function(s) by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1927
- e2e: Migrate duplicate node id test from kurtosis by @marun in https://github.com/ava-labs/avalanchego/pull/1573
- Add tracing levels to merkledb by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1933
- [x/merkledb] Add Configuration for `RootGenConcurrency` by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/1936
- e2e: Ensure testnet network dir is archived on failed test run by @marun in https://github.com/ava-labs/avalanchego/pull/1930
- Merkle db cleanup view creation by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1934
- Add async DB deletion helper by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1931
- Implement SDK handler to drop messages from non-validators by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1917
- Support proposervm historical block deletion by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1929
- Remove thread pool by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1940
- Merkledb split node storage into value and intermediate by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1918
- `merkledb` -- remove unneeded codec test helper by @danlaine in https://github.com/ava-labs/avalanchego/pull/1943
- `merkledb` -- add codec test and move helper by @danlaine in https://github.com/ava-labs/avalanchego/pull/1944
- Add throttler implementation to SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1905
- Add Throttled Handler implementation to SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1906
- Change merkledb caches to be size based by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1947
- Rename `node.marshal` to `node.bytes` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1951
- e2e: Switch to a default network node count of 2 by @marun in https://github.com/ava-labs/avalanchego/pull/1928
- MerkleDB Improve Node Size Calculation by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1950
- `merkledb` -- remove unneeded return values by @danlaine in https://github.com/ava-labs/avalanchego/pull/1959
- `sync` -- reduce test sizes by @danlaine in https://github.com/ava-labs/avalanchego/pull/1962
- `merkledb` -- limit number of goroutines calculating node IDs by @danlaine in https://github.com/ava-labs/avalanchego/pull/1960
- Add gossip package to p2p SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1958
- Improve state sync logging by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1955
- Update golang to 1.20.8 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1826
- Use odd-numbered request ids for SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1975
- update iterator invariant by @danlaine in https://github.com/ava-labs/avalanchego/pull/1978
- Document common usage of requestIDs for snow senders by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1981
- e2e: Diagnose and fix flakes by @marun in https://github.com/ava-labs/avalanchego/pull/1941
- `merkledb` -- `db_test.go` cleanup by @danlaine in https://github.com/ava-labs/avalanchego/pull/1954
- `merkledb` -- make config fields uints by @danlaine in https://github.com/ava-labs/avalanchego/pull/1963
- Only gracefully exit rpcchainvm server after Shutdown by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1988
- Add contexts to SDK callbacks by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1977
- Change max response size to target response size by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1995
- Add sdk gossip handler metrics by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1997
- Add p2p SDK Router metrics by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2000
- Merkledb Attempt to reduce test runtime by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1990
- longer timeout on windows UT by @danlaine in https://github.com/ava-labs/avalanchego/pull/2001
- `sync` -- log tweaks by @danlaine in https://github.com/ava-labs/avalanchego/pull/2008
- Add Validator Gossiper by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2015
- database: comment that Get returns ErrNotFound if key is not present by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/2018
- Return `height` from `GetCurrentSupply` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2022
- simplify platformvm `GetHeight` function by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2023
- Merkle db fix range proof commit bug by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2019
- Add `bag.Of` helper by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2027
- Cleanup early poll termination logic by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2029
- fix typo by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2030
- Merkle db intermediate node key compression by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1987
- Improve RPC Chain version mismatch error message by @martineckardt in https://github.com/ava-labs/avalanchego/pull/2021
- Move subnet owner lookup to platformvm state by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2024
- Fix fuzz tests; add iterator fuzz test by @danlaine in https://github.com/ava-labs/avalanchego/pull/1991
- Refactor subnet validator primary network requirements by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2014
- Rename events to event by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1973
- Add function to initialize SampleableSet by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/2017
- add `IsCortinaActivated` helper by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/2013
- Fix P-chain Import by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/2035
- Rename avm/blocks package to avm/block by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1970
- Merkledb Update rangeproof proto to be consistent with changeproof proto by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2040
- `merkledb` -- encode lengths as uvarints by @danlaine in https://github.com/ava-labs/avalanchego/pull/2039
- MerkleDB Remove GetNodeFromParent by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/2041

### New Contributors

- @martineckardt made their first contribution in https://github.com/ava-labs/avalanchego/pull/2021

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.9...v1.10.10

## [v1.10.9](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.9)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is updated to `28` all plugins must update to be compatible.

### Configs

- Changed the default value of `--network-compression-type` from `gzip` to `zstd`

### Fixes

- Marked corruptabledb as corrupted after encountering an error during iteration
- Fixed proposervm error handling during startup

### What's Changed

- `merkledb` -- verify range proof in fuzz test; fix bound error by @danlaine in https://github.com/ava-labs/avalanchego/pull/1789
- Update default compression type to zstd by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1839
- Migrate to `uber-go/mock` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1840
- `corruptabledb` -- corrupt on iterator error by @danlaine in https://github.com/ava-labs/avalanchego/pull/1829
- Add support for Maps to the reflect_codec by @nytzuga in https://github.com/ava-labs/avalanchego/pull/1790
- Make linter fail if `github.com/golang/mock/gomock` is used by @danlaine in https://github.com/ava-labs/avalanchego/pull/1843
- Firewoodize merkle db Part 1: Make Views ReadOnly by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1816
- E2E tests -- use appropriate timeouts by @danlaine in https://github.com/ava-labs/avalanchego/pull/1851
- e2e: Switch to testnet fixture by @marun in https://github.com/ava-labs/avalanchego/pull/1709
- `secp256k1` -- add fuzz tests by @danlaine in https://github.com/ava-labs/avalanchego/pull/1809
- Add fuzz test for complex codec unmarshalling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1846
- Simplify exported interface of the primary wallet by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1849
- Regenerate mocks by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1860
- Remove history btree by @danlaine in https://github.com/ava-labs/avalanchego/pull/1861
- `merkledb` -- Remove `CommitToParent` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1854
- `merkledb` -- remove other history btree by @danlaine in https://github.com/ava-labs/avalanchego/pull/1862
- `merkledb` -- add path fuzz test by @danlaine in https://github.com/ava-labs/avalanchego/pull/1852
- fix range proof verification case by @danlaine in https://github.com/ava-labs/avalanchego/pull/1834
- `merkledb` -- add change proof fuzz test; fix change proof verification by @danlaine in https://github.com/ava-labs/avalanchego/pull/1802
- Warp readme by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1780
- CODEOWNERS: add marun to tests by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1863
- Add CI check that auto-generated code is up to date by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1828
- `sync` -- change proof request can return range proof by @danlaine in https://github.com/ava-labs/avalanchego/pull/1772
- Ensure consistent use of best-practice `set -o` in all scripts by @marun in https://github.com/ava-labs/avalanchego/pull/1864
- GetCanonicalValidatorSet minimal ValidatorState iface by @darioush in https://github.com/ava-labs/avalanchego/pull/1875
- `sync` -- handle fatal error by @danlaine in https://github.com/ava-labs/avalanchego/pull/1874
- `merkledb` -- use `Maybe` for start bounds by @danlaine in https://github.com/ava-labs/avalanchego/pull/1872
- Add C-chain wallet to the primary network by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1850
- e2e: Refactor keychain and wallet creation to test helpers by @marun in https://github.com/ava-labs/avalanchego/pull/1870
- Update account nonce on exportTx accept by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1881
- `sync` -- add workheap test by @danlaine in https://github.com/ava-labs/avalanchego/pull/1879
- `merkledb` -- commit to db only by @danlaine in https://github.com/ava-labs/avalanchego/pull/1885
- Remove node/value lock from trieview by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1865
- remove old todo by @danlaine in https://github.com/ava-labs/avalanchego/pull/1892
- Fix race in TestHandlerDispatchInternal by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1895
- Remove duplicate code from proposervm block acceptance by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1894
- e2e: Bump permissionless subnets timeouts by @marun in https://github.com/ava-labs/avalanchego/pull/1897
- `merkledb` -- codec remove err checks by @danlaine in https://github.com/ava-labs/avalanchego/pull/1899
- Merkle db fix new return type by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1898
- Add SDK Sampling interface by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1877
- Add NoOpHandler implementation to SDK by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1903
- Remove unused scripts by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1908
- `merkledb` -- codec nits/cleanup by @danlaine in https://github.com/ava-labs/avalanchego/pull/1904
- `merkledb` -- preallocate `bytes.Buffer` in codec by @danlaine in https://github.com/ava-labs/avalanchego/pull/1900
- Proposervm height index repair fix by @abi87 in https://github.com/ava-labs/avalanchego/pull/1915
- `merkledb` -- move and rename methods by @danlaine in https://github.com/ava-labs/avalanchego/pull/1919
- Remove optional height indexing interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1896
- `merkledb` -- nits by @danlaine in https://github.com/ava-labs/avalanchego/pull/1916
- Fix code owners file by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1922
- Drop invalid TLS certs during initial handshake by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1923
- Restricted tls metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1924

### New Contributors

- @nytzuga made their first contribution in https://github.com/ava-labs/avalanchego/pull/1790

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.8...v1.10.9

## [v1.10.8](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.8)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `27` and compatible with versions `v1.10.5 - v1.10.7`.

**This update changes the local network genesis. This version will not be able to join local networks with prior versions.**

**The first startup of the P-Chain will perform indexing operations. This indexing runs in the background and does not impact restart time. During this indexing the node will report increased CPU, memory, and disk usage.**

### APIs

- Added `platform.getBlockByHeight`

### Configs

- Added `--partial-sync-primary-network` flag to enable non-validators to optionally sync only the P-chain on the primary network
- Added P-chain cache size configuration `block-id-cache-size`

### Fixes

- Fixed P-chain GetValidatorSet regression for subnets
- Changed `x/sync` range/change proof bounds from `[]byte` to `Maybe[[]byte]`
- Fixed `x/sync` error handling from failure to send app messages

### What's Changed

- Removes calls to ctrl.Finish by @darioush in https://github.com/ava-labs/avalanchego/pull/1803
- e2e: Remove unnecessary transaction status checking by @marun in https://github.com/ava-labs/avalanchego/pull/1786
- fix p2p mockgen location by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1806
- fix end proof verification by @danlaine in https://github.com/ava-labs/avalanchego/pull/1801
- `merkledb` -- add proof fuzz test by @danlaine in https://github.com/ava-labs/avalanchego/pull/1804
- `sync` -- re-add network client metrics by @danlaine in https://github.com/ava-labs/avalanchego/pull/1787
- Add function to initialize set from elements by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1808
- Add Maybe to the end bound of proofs (Part 1) by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1793
- add go version to --version by @amirhasanzadehpy in https://github.com/ava-labs/avalanchego/pull/1819
- e2e: Add local network fixture by @marun in https://github.com/ava-labs/avalanchego/pull/1700
- Fix test flake in TestProposalTxsInMempool by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1822
- `sync` -- remove todo by @danlaine in https://github.com/ava-labs/avalanchego/pull/1788
- Add Maybe to the end bound of proofs (Part 2) by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1813
- Move Maybe to its own package by @danlaine in https://github.com/ava-labs/avalanchego/pull/1817
- `merkledb` -- clarify/improve change proof invariants by @danlaine in https://github.com/ava-labs/avalanchego/pull/1810
- P-chain state prune + height index by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1719
- Update maintainer of the debian packages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1825
- Make platformvm implement `block.HeightIndexedChainVM` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1746
- Add P-chain `GetBlockByHeight` API method by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1747
- Update local genesis startTime by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1811
- `sync` -- add handling for fatal error by @danlaine in https://github.com/ava-labs/avalanchego/pull/1690
- Add error logs for unexpected proposervm BuildBlock failures by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1832
- Fix subnet validator set public key initialization by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1833
- Document PendingTxs + BuildBlock consensus engine requirement by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1835
- Bump github.com/supranational/blst from 0.3.11-0.20230406105308-e9dfc5ee724b to 0.3.11 by @dependabot in https://github.com/ava-labs/avalanchego/pull/1831
- Add Primary Network Lite Sync Option by @abi87 in https://github.com/ava-labs/avalanchego/pull/1769
- Check P-chain ShouldPrune during Initialize by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1836

### New Contributors

- @amirhasanzadehpy made their first contribution in https://github.com/ava-labs/avalanchego/pull/1819
- @dependabot made their first contribution in https://github.com/ava-labs/avalanchego/pull/1831

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.7...v1.10.8

## [v1.10.7](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.7)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). This release contains meaningful performance improvements and we recommend updating as soon as possible.

The plugin version is unchanged at `27` and compatible with versions `v1.10.5 - v1.10.6`.

### APIs

- Modified `platform.getValidatorsAt` to also return BLS public keys

### Configs

- Changed the default value of `--network-allow-private-ips` to `false` when the `--network-id` is either `fuji` or `mainnet`
- Added P-chain cache size configurations
  - `block-cache-size`
  - `tx-cache-size`
  - `transformed-subnet-tx-cache-size`
  - `reward-utxos-cache-size`
  - `chain-cache-size`
  - `chain-db-cache-size`
- Removed various long deprecated flags
  - `--genesis` use `--genesis-file` instead
  - `--genesis-content` use `--genesis-file-content` instead
  - `--inbound-connection-throttling-cooldown` use `--network-inbound-connection-throttling-cooldown` instead
  - `--inbound-connection-throttling-max-conns-per-sec` use `--network-inbound-connection-throttling-max-conns-per-sec` instead
  - `--outbound-connection-throttling-rps` use `network-outbound-connection-throttling-rps` instead
  - `--outbound-connection-timeout` use `network-outbound-connection-timeout` instead
  - `--staking-enabled` use `sybil-protection-enabled` instead
  - `--staking-disabled-weight` use `sybil-protection-disabled-weight` instead
  - `--network-compression-enabled` use `--network-compression-type` instead
  - `--consensus-gossip-frequency` use `--consensus-accepted-frontier-gossip-frequency` instead

### Fixes

- Fixed C-chain tx tracer crashes
- Fixed merkledb panic during state sync
- Fixed merkledb state sync stale target tracking

### What's Changed

- Remove deprecated configs by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1712
- upgrade: Increase all ANR timeouts to 2m to ensure CI reliability by @marun in https://github.com/ava-labs/avalanchego/pull/1737
- fix sync panic by @danlaine in https://github.com/ava-labs/avalanchego/pull/1736
- remove `vm.state` re-assignment in tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1739
- Expose BLS public keys from platform.getValidatorsAt by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1740
- Fix validator set diff tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1744
- Replace List() with Map() on validators.Set by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1745
- vms/platformvm: configure state cache sizes #1522 by @najeal in https://github.com/ava-labs/avalanchego/pull/1677
- Support both `stateBlk`s and `Block`s in `blockDB` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1748
- Add `DefaultExecutionConfig` var to `platformvm` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1749
- Remove hanging TODO from prior change by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1758
- Write process context on node start to simplify test orchestration by @marun in https://github.com/ava-labs/avalanchego/pull/1729
- x/sync: add locks for peerTracker by @darioush in https://github.com/ava-labs/avalanchego/pull/1756
- Add ids length constants by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1759
- [x/sync] Update target locking by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/1763
- Export warp errors for external use by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1771
- Remove unused networking constant by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1774
- Change the default value of `--network-allow-private-ips` to `false` for `mainnet` and `fuji` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1773
- Remove context.TODO from tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1778
- Replace linkeddb iterator with native DB range queries by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1752
- Add support for measuring key size in caches by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1781
- Bump coreth to v0.12.5-rc.0 by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1775
- Add metric for the number of elements in a cache by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1782
- Evict blocks based on size by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1766
- Add proposervm state metrics by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1785
- Register metercacher `len` metric by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1791
- Reduce block cache sizes to 64 MiB by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1794
- Add p2p sdk by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1799

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.5...v1.10.7

## [v1.10.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.5)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is updated to `27` all plugins must update to be compatible.

**The first startup of the X-Chain will perform an indexing operation. This indexing runs in the background and does not impact restart time.**

### APIs

- Added `avalanche_network_clock_skew_sum` metric
- Added `avalanche_network_clock_skew_count` metric

### Configs

- Added `--tracing-headers` to allow specifying headers to the tracing indexer

### Fixes

- Fixed API handler crash for `lookupState` in `prestate` tracer
- Fixed API handler crash for LOG edge cases in the `callTracer`

### What's Changed

- stop persisting rejected blocks on P-chain by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1696
- Ensure scripts/lint.sh failure when used with incompatible grep by @marun in https://github.com/ava-labs/avalanchego/pull/1711
- sum peers clock skew into metric by @najeal in https://github.com/ava-labs/avalanchego/pull/1695
- Make AVM implement `block.HeightIndexedChainVM` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1699
- ProposerVM nits by @abi87 in https://github.com/ava-labs/avalanchego/pull/1688
- Sorting -- Remove old `IsSortedAndUnique`, rename `IsSortedAndUniqueSortable` to `IsSortedAndUnique` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1666
- Update snow consensus doc post X-chain linearization by @exdx in https://github.com/ava-labs/avalanchego/pull/1703
- `merkledb` / `sync` -- remove TODOs by @danlaine in https://github.com/ava-labs/avalanchego/pull/1718
- remove cache TODOs by @danlaine in https://github.com/ava-labs/avalanchego/pull/1721
- Adjust `NewSizedCache` to take in a size function by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1725
- Wallet issuance to return tx instead of tx id by @felipemadero in https://github.com/ava-labs/avalanchego/pull/1704
- Add support for providing tracing headers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1727
- Only return accepted blocks in `GetStatelessBlock` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1724
- Proposermv fix goroutine leaks by @abi87 in https://github.com/ava-labs/avalanchego/pull/1713
- Update warp msg format by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1686
- Cleanup anr scripts by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1714
- remove TrackBandwidth from NetworkClient by @danlaine in https://github.com/ava-labs/avalanchego/pull/1716
- Bump network start timeout by @marun in https://github.com/ava-labs/avalanchego/pull/1730
- e2e: Ensure e2e.test is built with portable BLST by @marun in https://github.com/ava-labs/avalanchego/pull/1734
- e2e: Increase all ANR timeouts to 2m to ensure CI reliability. by @marun in https://github.com/ava-labs/avalanchego/pull/1733

### New Contributors

- @exdx made their first contribution in https://github.com/ava-labs/avalanchego/pull/1703

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.4...v1.10.5

## [v1.10.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.4)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged.

The plugin version is unchanged at `26` and compatible with versions `v1.10.1 - v1.10.3`.

**The first startup of the X-Chain will perform a pruning operation. This pruning runs in the background and does not impact restart time.**

### APIs

- Removed `avalanche_X_vm_avalanche_metervm_pending_txs_count` metric
- Removed `avalanche_X_vm_avalanche_metervm_pending_txs_sum` metric
- Removed `avalanche_X_vm_avalanche_metervm_get_tx_count` metric
- Removed `avalanche_X_vm_avalanche_metervm_get_tx_sum` metric
- Removed `avalanche_X_vm_avalanche_metervm_get_tx_err_count` metric
- Removed `avalanche_X_vm_avalanche_metervm_get_tx_err_sum` metric

### Configs

- Added `--staking-host` to allow binding only on a specific address for staking
- Added `checksums-enabled` to the X-chain and P-chain configs

### Fixes

- Fixed `proposervm` `preForkBlock.Status()` response after the fork has occurred
- Fixed C-chain logs collection error when no receipts occur in a block
- Fixed merkledb's `findNextKey` when an empty end proof is provided
- Fixed 0 length key issues with proof generation and verification
- Fixed Docker execution on non-amd64 architectures

### What's Changed

- e2e: Support testing on MacOS without requiring firewall exceptions by @marun in https://github.com/ava-labs/avalanchego/pull/1613
- Reduce resource log level by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1622
- Improve `snow/` tests with `require` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1503
- Improve `x/` tests with `require` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1454
- `sync` -- fix `TestFindNextKeyRandom` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1624
- Improve `vms/` tests with `require` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1505
- Improve `database/` tests with `require` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1506
- Ban usage of `t.Fatal` and `t.Error` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1453
- chore: fix typo in binary_snowflake.go by @eltociear in https://github.com/ava-labs/avalanchego/pull/1630
- Discriminate window fit err msg from overdelegated error msg by @felipemadero in https://github.com/ava-labs/avalanchego/pull/1606
- Remove MaxConnectionAge gRPC StreamID overflow mitigation by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1388
- add fuzzing action by @danlaine in https://github.com/ava-labs/avalanchego/pull/1635
- Remove dagState and GetUTXOFromID by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1632
- Update all AVM tests for post-linearization by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1631
- Remove PendingTxs from the DAGVM interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1641
- Remove GetTx from the DAGVM interface by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1642
- Bump coreth v0.12.4 by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1646
- [x/merkledb] Remove useless `err` check by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/1650
- [x/merkledb] Trailing whitespace removal on README by @patrick-ogrady in https://github.com/ava-labs/avalanchego/pull/1649
- Remove unneeded functions from UniqueTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1643
- Simplify tx verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1654
- `merkledb` --  fix `findNextKey` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1653
- Cleanup X-chain UniqueTx Dependencies by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1656
- Prune X-chain State by @coffeeavax in https://github.com/ava-labs/avalanchego/pull/1427
- Support building docker image on ARM64 by @dshiell in https://github.com/ava-labs/avalanchego/pull/1103
- remove goreleaser by @danlaine in https://github.com/ava-labs/avalanchego/pull/1660
- Fix Dockerfile on non amd64 platforms by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1661
- Improve metrics error message by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1663
- Remove X-chain UniqueTx by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1662
- Add state checksums by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1658
- Modify proposervm window by @najeal in https://github.com/ava-labs/avalanchego/pull/1638
- sorting nit by @danlaine in https://github.com/ava-labs/avalanchego/pull/1665
- `merkledb` -- rewrite and test range proof invariants; fix proof generation/veriifcation bugs by @danlaine in https://github.com/ava-labs/avalanchego/pull/1629
- Add minimum proposer window length by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1667
- CI -- only run fuzz tests on ubuntu by @danlaine in https://github.com/ava-labs/avalanchego/pull/1636
- `MerkleDB` -- remove codec version by @danlaine in https://github.com/ava-labs/avalanchego/pull/1671
- `MerkleDB` -- use default config in all tests by @danlaine in https://github.com/ava-labs/avalanchego/pull/1590
- `sync` -- reduce stuttering by @danlaine in https://github.com/ava-labs/avalanchego/pull/1672
- `Sync` -- unexport field by @danlaine in https://github.com/ava-labs/avalanchego/pull/1673
- `sync` -- nits and cleanup by @danlaine in https://github.com/ava-labs/avalanchego/pull/1674
- `sync` -- remove unused code by @danlaine in https://github.com/ava-labs/avalanchego/pull/1676
- Mark preForkBlocks after the fork as Rejected by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1683
- `merkledb` -- fix comment by @danlaine in https://github.com/ava-labs/avalanchego/pull/1675
- `MerkleDB` -- document codec by @danlaine in https://github.com/ava-labs/avalanchego/pull/1670
- `sync` -- client cleanup by @danlaine in https://github.com/ava-labs/avalanchego/pull/1680
- Update buf version to v1.23.1 by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1685

### New Contributors

- @eltociear made their first contribution in https://github.com/ava-labs/avalanchego/pull/1630
- @felipemadero made their first contribution in https://github.com/ava-labs/avalanchego/pull/1606
- @dshiell made their first contribution in https://github.com/ava-labs/avalanchego/pull/1103
- @najeal made their first contribution in https://github.com/ava-labs/avalanchego/pull/1638

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.3...v1.10.4

## [v1.10.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.3)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged. The supported plugin version is `26`.

**Users must specify the `--allowed-hosts-flag` to receive inbound API traffic from non-local hosts.**

### APIs

- Added health metrics based on tags
  - `avalanche_health_checks_failing{tag="TAG"}`
  - `avalanche_liveness_checks_failing{tag="TAG"}`
  - `avalanche_readiness_checks_failing{tag="TAG"}`
- Removed P-chain VM percent connected metrics
  - `avalanche_P_vm_percent_connected`
  - `avalanche_P_vm_percent_connected_subnet{subnetID="SUBNETID"}`
- Added percent connected metrics by chain
  - `avalanche_{ChainID}_percent_connected`
- Removed `avalanche_network_send_queue_portion_full` metric

### Configs

- Added `--http-allowed-hosts` with a default value of `localhost`
- Removed `--snow-mixed-query-num-push-vdr`
- Removed `--snow-mixed-query-num-push-non-vdr`
- Removed `minPercentConnectedStakeHealthy` from the subnet config

### Fixes

- Fixed `platformvm.GetValidatorSet` returning incorrect BLS public keys
- Fixed IPv6 literal binding with `--http-host`
- Fixed P2P message log format

### What's Changed

- `x/sync` -- Add proto for P2P messages  by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1472
- Bump Protobuf and tooling and add section to proto docs outlining buf publishing by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1552
- Minor pchain UTs cleanup by @abi87 in https://github.com/ava-labs/avalanchego/pull/1554
- Add ping uptimes test by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1550
- Add workflow to mark stale issues and PRs by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1443
- Enforce inlining functions with a single error return in `require.NoError` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1500
- `x/sync` / `x/merkledb` -- add `SyncableDB` interface by @danlaine in https://github.com/ava-labs/avalanchego/pull/1555
- Rename beacon to boostrapper, define bootstrappers in JSON file for cross-language compatibility by @gyuho in https://github.com/ava-labs/avalanchego/pull/1439
- add P-chain height indexing by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1447
- Add P-chain `GetBlockByHeight` API method by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1448
- `x/sync` -- use for sending Range Proofs by @danlaine in https://github.com/ava-labs/avalanchego/pull/1537
- Add test to ensure that database packing produces sorted values by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1560
- Randomize unit test execution order to identify unwanted dependency by @marun in https://github.com/ava-labs/avalanchego/pull/1565
- use `http.Error` instead of separately writing error code and message by @danlaine in https://github.com/ava-labs/avalanchego/pull/1564
- Adding allowed http hosts flag by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1566
- `x/sync` -- Use proto for sending Change Proofs by @danlaine in https://github.com/ava-labs/avalanchego/pull/1541
- Only send `PushQuery` messages after building the block by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1428
- Rename APIAllowedOrigins to HTTPAllowedOrigins by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1567
- Add GetBalance examples for the P-chain and X-chain wallets by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1569
- Reduce number of test iterations by @danlaine in https://github.com/ava-labs/avalanchego/pull/1568
- Re-add upgrade tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1410
- Remove lists from Chits messages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1412
- Add more X-chain tests by @coffeeavax in https://github.com/ava-labs/avalanchego/pull/1487
- fix typo by @meaghanfitzgerald in https://github.com/ava-labs/avalanchego/pull/1570
- Reduce the number of test health checks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1571
- Fix proposervm.GetAncestors test flake by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1572
- Remove list from AcceptedFrontier message by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1578
- Remove version db from merkle db by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1534
- `MerkleDB` -- add eviction batch size config by @danlaine in https://github.com/ava-labs/avalanchego/pull/1586
- `MerkleDB` -- fix `onEvictCache.Flush` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1589
- Revert P-Chain height index by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1591
- `x/sync` -- Add `SyncableDB` proto by @danlaine in https://github.com/ava-labs/avalanchego/pull/1559
- Clarify break on error during ancestors lookup by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1580
- Add buf-push github workflow by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1556
- Pchain bls key diff fix by @abi87 in https://github.com/ava-labs/avalanchego/pull/1584
- Cleanup fx interface compliance by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1599
- Improve metrics error msging by @anusha-ctrl in https://github.com/ava-labs/avalanchego/pull/1598
- Separate health checks by tags by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1579
- Separate subnet stake connected health and metrics from P-chain by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1358
- Merkle db iterator by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1533
- Fix unreadable message errors by @morrisettjohn in https://github.com/ava-labs/avalanchego/pull/1585
- Log unexpected errors during GetValidatorSet by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1592
- `merkleDB` -- add inner heap type to syncWorkHeap by @danlaine in https://github.com/ava-labs/avalanchego/pull/1582
- `sync` -- explain algorithm in readme by @danlaine in https://github.com/ava-labs/avalanchego/pull/1600
- Rename license header file to avoid unintended license indexing by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1608
- `merkledb` and `sync` -- use time based rand seed by @danlaine in https://github.com/ava-labs/avalanchego/pull/1607
- add `local-prefixes` setting for `goimports` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1612
- snow/engine/snowman: instantiate voter after issuer by @gyuho in https://github.com/ava-labs/avalanchego/pull/1610
- Update CodeQL to v2 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1616
- Remove old networking metric by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1619
- Fix --http-host flag to support IPv6 by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1620

### New Contributors

- @marun made their first contribution in https://github.com/ava-labs/avalanchego/pull/1565
- @meaghanfitzgerald made their first contribution in https://github.com/ava-labs/avalanchego/pull/1570
- @anusha-ctrl made their first contribution in https://github.com/ava-labs/avalanchego/pull/1598
- @morrisettjohn made their first contribution in https://github.com/ava-labs/avalanchego/pull/1585

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.2...v1.10.3

## [v1.10.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.2)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged. The supported plugin version is `26`.

### APIs

- Significantly improved the performance of `platform.getStake`
- Added `portion_filled` metric for all metered caches
- Added resource metrics by process
  - `avalanche_system_resources_num_cpu_cycles`
  - `avalanche_system_resources_num_disk_read_bytes`
  - `avalanche_system_resources_num_disk_reads`
  - `avalanche_system_resources_num_disk_write_bytes`
  - `avalanche_system_resources_num_disk_writes`

### Configs

- Deprecated `--genesis` in favor of `--genesis-file`
- Deprecated `--genesis-content` in favor of `--genesis-file-content`
- Deprecated `--inbound-connection-throttling-cooldown` in favor of `--network-inbound-connection-throttling-cooldown`
- Deprecated `--inbound-connection-throttling-max-conns-per-sec` in favor of `--network-inbound-connection-throttling-max-conns-per-sec`
- Deprecated `--outbound-connection-throttling-rps` in favor of `--network-outbound-connection-throttling-rps`
- Deprecated `--outbound-connection-timeout` in favor of `--network-outbound-connection-timeout`
- Deprecated `--staking-enabled` in favor of `--sybil-protection-enabled`
- Deprecated `--staking-disabled-weight` in favor of `--sybil-protection-disabled-weight`
- Deprecated `--consensus-gossip-frequency` in favor of `--consensus-accepted-frontier-gossip-frequency`

### Fixes

- Fixed `--network-compression-type` to correctly honor the requested compression type, rather than always using gzip
- Fixed CPU metrics on macos

### What's Changed

- use `require` library functions in tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1451
- style nits in vm clients by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1449
- utils/logging: add "Enabled" method to remove redundant verbo logs by @gyuho in https://github.com/ava-labs/avalanchego/pull/1461
- ban `require.EqualValues` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1457
- chains: do not hold write subnetsLock in health checks by @gyuho in https://github.com/ava-labs/avalanchego/pull/1460
- remove zstd check by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1459
- use `require.IsType` for type assertions in tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1458
- vms/platformvm/service: nits (preallocate address slice, error msg) by @gyuho in https://github.com/ava-labs/avalanchego/pull/1477
- ban `require.NotEqualValues` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1470
- use `require` in `api` and `utils/password` packages by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1471
- use "golang.org/x/term" as "golang.org/x/crypto/ssh/terminal" is deprecated by @gyuho in https://github.com/ava-labs/avalanchego/pull/1464
- chains: move "msgChan" closer to the first use (readability) by @gyuho in https://github.com/ava-labs/avalanchego/pull/1484
- ban function params for `require.ErrorIs` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1486
- standardize imports by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1466
- fix license header test by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1492
- use blank identifier for interface compliance by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1493
- codec: remove "SetMaxSize" from "Manager", remove unnecessary lock by @gyuho in https://github.com/ava-labs/avalanchego/pull/1481
- config: disallow "ThrottlerConfig.MaxRecheckDelay" < 1 ms by @gyuho in https://github.com/ava-labs/avalanchego/pull/1435
- ban `require.Equal` when testing for `0` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1495
- Clean up MerkleDVB Sync Close lock by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1469
- MerkleDB Cleanup by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1465
- Remove comment referencing old IP based tracking by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1509
- ban usage of `require.Len` when testing for length `0` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1496
- ban usage of `require.Equal` when testing for length by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1497
- ban usage of `nil` in require functions by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1498
- Sized LRU cache by @abi87 in https://github.com/ava-labs/avalanchego/pull/1517
- engine/snowman: clean up some comments in "bubbleVotes" unit tests by @gyuho in https://github.com/ava-labs/avalanchego/pull/1444
- snow/networking/sender: add missing verbo check by @gyuho in https://github.com/ava-labs/avalanchego/pull/1504
- Delete duplicate test var definitions by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1518
- utils/bag: print generic type for bag elements by @gyuho in https://github.com/ava-labs/avalanchego/pull/1507
- Fix incorrect test refactor by @abi87 in https://github.com/ava-labs/avalanchego/pull/1526
- Pchain validators repackaging by @abi87 in https://github.com/ava-labs/avalanchego/pull/1284
- Config overhaul by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1370
- rename enabled staking to sybil protection enabled by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1441
- Fix network compression type flag usage by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1532
- Deprecate uptimes in pong message by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1362
- Add CPU cycles and number of disk read/write metrics by pid by @coffeeavax in https://github.com/ava-labs/avalanchego/pull/1334
- Fetch process resource stats as best-effort by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1543
- Add serialization tests for transactions added in Banff by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1513
- Log chain shutdown duration by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1545
- add interface for MerkleDB by @danlaine in https://github.com/ava-labs/avalanchego/pull/1519

### New Contributors

- @gyuho made their first contribution in https://github.com/ava-labs/avalanchego/pull/1461
- @coffeeavax made their first contribution in https://github.com/ava-labs/avalanchego/pull/1334

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.1...v1.10.2

## [v1.10.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.1)

This version is backwards compatible to [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0). It is optional, but encouraged. The supported plugin version is `26`.

### APIs

- Enabled `avm.getBlockByHeight` to take in `height` as a string
- Added IDs to json formats
  - `platform.getTx` now includes `id` in the `tx` response
  - `platform.getBlock` now includes `id` in the `block` response and in the internal `tx` fields
  - `avm.getTx` now includes `id` in the `tx` response
  - `avm.getBlock` now includes `id` in the `block` response and in the internal `tx` fields
  - `avm.getBlockByHeight` now includes `id` in the `block` response and in the internal `tx` fields
- Removed `avm.issueStopVertex`
- Fixed `wallet` methods to correctly allow issuance of dependent transactions after the X-chain linearization
- Added `validatorOnly` flag in `platform.getStake`
- Removed all avalanche consensus metrics
- Fixed `msgHandlingTime` metrics

### Configs

- Removed `--snow-avalanche-num-parents`
- Removed `--snow-avalanche-batch-size`

### Fixes

- Fixed panic when restarting partially completed X-chain snowman bootstrapping
- Fixed `--network-allow-private-ips` handling to correctly prevent outbound connections to private IP ranges
- Fixed UniformSampler to support sampling numbers between MaxInt64 and MaxUint64
- Fixed data race in txID access during transaction gossip in the AVM

### What's Changed

- Add benchmark for gRPC GetValidatorSet by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1326
- Add checks for database being closed in merkledb; other nits by @danlaine in https://github.com/ava-labs/avalanchego/pull/1333
- Update linkedhashmap to only Rlock when possible by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1329
- Remove no-op changes from history results by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1335
- Cleanup type assertions in the linkedHashmap by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1341
- Fix racy avm tx access by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1349
- Update Fuji beacon ips by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1354
- Remove duplicate TLS verification by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1364
- Adjust Merkledb Trie invalidation locking by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1355
- Use require in Avalanche bootstrapping tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1344
- Add Proof size limit to sync client by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1269
- Add stake priority helpers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1375
- add contribution file by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1373
- Remove max sample value by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1374
- Prefetch rpcdb iterator batches by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1323
- Temp fix for flaky Sync Test by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1378
- Update merkle cache to be FIFO instead of LRU by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1353
- Improve cost of BLS key serialization for gRPC by @hexfusion in https://github.com/ava-labs/avalanchego/pull/1343
- [Issue-1368]: Panic in serializedPath.HasPrefix by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1371
- Add ValidatorsOnly flag to GetStake by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1377
- Use proto in `x/sync` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1336
- Update incorrect fuji beacon IPs by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1392
- Update `api/` error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1393
- refactor concurrent work limiting in sync in `x/sync` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1347
- Remove check for impossible condition in `x/sync` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1348
- Improve `codec/` error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1396
- Improve `config/` error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1397
- Improve `genesis/` error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1398
- Improve various error handling locations by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1399
- Improve `utils/` error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1400
- Improve consensus error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1401
- Improve secp256k1fx + merkledb error handling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1402
- Ban usage of require.Error by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1346
- Remove slice capacity hint in `x/sync` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1350
- Simplify `syncWorkHeap` less function in `x/sync` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1351
- Replace `switch` with `txs.Visitor` in X chain signer by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1404
- Include IDs in json marshalling by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1408
- Adjust find next key logic in x/Sync by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1331
- Remove bitmask from writeMsgLen by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1342
- Require `txID`s in PeerList messages by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1411
- Allow dependent tx issuance over the wallet API by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1413
- Add support for proto `message.Tx` decoding by @danlaine in https://github.com/ava-labs/avalanchego/pull/1332
- Remove avalanche bootstrapping -> avalanche consensus transition by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1345
- Benchmark get canonical validator set by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1417
- Simplify IP status calculation by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1421
- Honor AllowPrivateIPs config by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1422
- Update BLS signature ordering to avoid public key compression by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1416
- Remove DAG based consensus by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1359
- Remove IssueStopVertex message by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1419
- Fix msgHandlingTime by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1432
- Change ChangeProofs to only have one list of key/value change instead of key/values and deleted by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1385
- Update AMI generation workflow by @charlie-ava in https://github.com/ava-labs/avalanchego/pull/1289
- Support `height` as a string in `avm.getBlockByHeight` by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1437
- Defer Snowman Bootstrapper parser initialization to Start by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1442
- Cleanup proposervm ancestors packing @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1446

### New Contributors

- @hexfusion made their first contribution in https://github.com/ava-labs/avalanchego/pull/1326

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.10.0...v1.10.1

## [v1.10.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.10.0)

[This upgrade](https://medium.com/avalancheavax/cortina-x-chain-linearization-a1d9305553f6) linearizes the X-chain, introduces delegation batching to the P-chain, and increases the maximum block size on the C-chain.

The changes in the upgrade go into effect at 11 AM ET, April 25th 2023 on Mainnet.

**All Mainnet nodes should upgrade before 11 AM ET, April 25th 2023.**

The supported plugin version is `25`.

### What's Changed

- Add CODEOWNERS for the x/ package by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1260
- Feature Spec Template by @richardpringle in https://github.com/ava-labs/avalanchego/pull/1258
- Standardize CI triggers by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1265
- special case no sent/received message in network health check by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1263
- Fix bug template by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1268
- Replace `flags` usage with `pflags` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1270
- Fixed grammatical errors in `README.md` by @krakxn in https://github.com/ava-labs/avalanchego/pull/1102
- Add tests for race conditions in merkledb by @kyl27 in https://github.com/ava-labs/avalanchego/pull/1256
- Add P-chain indexer API example by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1271
- use `require` in `snow/choices` tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1279
- use `require` in `utils/wrappers` tests by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1280
- add support for tracking delegatee rewards to validator metadata by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1273
- defer delegatee rewards until end of validator staking period by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1262
- Initialize UptimeCalculator in TestPeer by @joshua-kim in https://github.com/ava-labs/avalanchego/pull/1283
- Add Avalanche liveness health checks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1287
- Skip AMI generation with Fuji tags by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1288
- Use `maps.Equal` in `set.Equals` by @danlaine in https://github.com/ava-labs/avalanchego/pull/1290
- return accrued delegator rewards in `GetCurrentValidators` by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1291
- Add zstd compression by @danlaine in https://github.com/ava-labs/avalanchego/pull/1278
- implement `txs.Visitor` in X chain wallet by @dhrubabasu in https://github.com/ava-labs/avalanchego/pull/1299
- Parallelize gzip compression by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1293
- Add zip bomb tests by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1300
- Gossip Avalanche frontier after the linearization by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1303
- Add fine grained metrics+logging for handling, processing, and grab l… by @aaronbuchwald in https://github.com/ava-labs/avalanchego/pull/1301
- Persist stateless block in AVM state by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1305
- Initialize FxID fields in GetBlock and GetBlockByHeight by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1306
- Filterable Health Tags by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1304
- increase health await timeout by @ceyonur in https://github.com/ava-labs/avalanchego/pull/1317
- Expose GetEngineManager from the chain Handler by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1316
- Add BLS benchmarks by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1318
- Encode codec version in merkledb by @danlaine in https://github.com/ava-labs/avalanchego/pull/1313
- Expose consensus-app-concurrency by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1322
- Adjust Logic In Merkle DB History by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1310
- Fix Concurrency Bug In CommitToParent by @dboehm-avalabs in https://github.com/ava-labs/avalanchego/pull/1320
- Cleanup goroutines on health.Stop by @StephenButtolph in https://github.com/ava-labs/avalanchego/pull/1325

### New Contributors

- @richardpringle made their first contribution in https://github.com/ava-labs/avalanchego/pull/1258
- @ceyonur made their first contribution in https://github.com/ava-labs/avalanchego/pull/1263
- @krakxn made their first contribution in https://github.com/ava-labs/avalanchego/pull/1102
- @kyl27 made their first contribution in https://github.com/ava-labs/avalanchego/pull/1256
- @dhrubabasu made their first contribution in https://github.com/ava-labs/avalanchego/pull/1279
- @joshua-kim made their first contribution in https://github.com/ava-labs/avalanchego/pull/1283
- @dboehm-avalabs made their first contribution in https://github.com/ava-labs/avalanchego/pull/1310

**Full Changelog**: https://github.com/ava-labs/avalanchego/compare/v1.9.16...v1.10.0

## [v1.9.16](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.16)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

- Removed unnecessary repoll after rejecting vertices
- Improved snowstorm lookup error handling
- Removed rejected vertices from the Avalanche frontier more aggressively
- Reduced default health check values for processing decisions

## [v1.9.15](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.15)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

- Fixed `x/merkledb.ChangeProof#getLargestKey` to correctly handle no changes
- Added test for `avm/txs/executor.SemanticVerifier#verifyFxUsage` with multiple valid fxs
- Fixed CPU + bandwidth performance regression during vertex processing
- Added example usage of the `/ext/index/X/block` API
- Reduced the default value of `--snow-optimal-processing` from `50` to `10`
- Updated the year in the license header

## [v1.9.14](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.14)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

## [v1.9.13](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.13)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

## [v1.9.12](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

### Networking

- Removed linger setting on P2P connections
- Improved error message when failing to calculate peer uptimes
- Removed `EngineType` from P2P response messages
- Added context cancellation during dynamic IP updates
- Reduced the maximum P2P reconnect delay from 1 hour to 1 minute

### Consensus

- Added support to switch from `Avalanche` consensus to `Snowman` consensus
- Added support for routing consensus messages to either `Avalanche` or `Snowman` consensus on the same chain
- Removed usage of deferred evaluation of the `handler.Consensus` in the `Avalanche` `OnFinished` callback
- Dropped inbound `Avalanche` consensus messages after switching to `Snowman` consensus
- Renamed the `Avalanche` VM metrics prefix from `avalanche_{chainID}_vm_` to `avalanche_{chainID}_vm_avalanche`
- Replaced `consensus` and `decision` dispatchers with `block`, `tx`, and `vertex` dispatchers
- Removed `Avalanche` bootstrapping restarts during the switch to `Snowman` consensus

### AVM

- Added `avm` block execution manager
- Added `avm` block builder
- Refactored `avm` transaction syntactic verification
- Refactored `avm` transaction semantic verification
- Refactored `avm` transaction execution
- Added `avm` mempool gossip
- Removed block timer interface from `avm` `mempool`
- Moved `toEngine` channel into the `avm` `mempool`
- Added `GetUTXOFromID` to the `avm` `state.Chain` interface
- Added unpopulated `MerkleRoot` to `avm` blocks
- Added `avm` transaction based metrics
- Replaced error strings with error interfaces in the `avm` mempool

### PlatformVM

- Added logs when the local nodes stake amount changes
- Moved `platformvm` `message` package into `components`
- Replaced error strings with error interfaces in the `platformvm` mempool

### Warp

- Added `ID` method to `warp.UnsignedMessage`
- Improved `warp.Signature` verification error descriptions

### Miscellaneous

- Improved `merkledb` locking to allow concurrent read access through `trieView`s
- Fixed `Banff` transaction signing with ledger when using the wallet
- Emitted github artifacts after successful builds
- Added non-blocking bounded queue
- Converted the `x.Parser` helper to be a `block.Parser` interface from a `tx.Parser` interface

### Cleanup

- Separated dockerhub image publishing from the kurtosis test workflow
- Exported various errors to use in testing
- Removed the `vms/components/state` package
- Replaced ad-hoc linked hashmaps with the standard data-structure
- Removed `usr/local/lib/avalanche` from deb packages
- Standardized usage of `constants.UnitTestID`

### Examples

- Added P-chain `RemoveSubnetValidatorTx` example using the wallet
- Added X-chain `CreateAssetTx` example using the wallet

### Configs

- Added support to specify `HTTP` server timeouts
  - `--http-read-timeout`
  - `--http-read-header-timeout`
  - `--http-write-timeout`
  - `--http-idle-timeout`

### APIs

- Added `avm` block APIs
  - `avm.getBlock`
  - `avm.getBlockByHeight`
  - `avm.getHeight`
- Converted `avm` APIs to only surface accepted state
- Deprecated all `ipcs` APIs
  - `ipcs.publishBlockchain`
  - `ipcs.unpublishBlockchain`
  - `ipcs.getPublishedBlockchains`
- Deprecated all `keystore` APIs
  - `keystore.createUser`
  - `keystore.deleteUser`
  - `keystore.listUsers`
  - `keystore.importUser`
  - `keystore.exportUser`
- Deprecated the `avm/pubsub` API endpoint
- Deprecated various `avm` APIs
  - `avm.getAddressTxs`
  - `avm.getBalance`
  - `avm.getAllBalances`
  - `avm.createAsset`
  - `avm.createFixedCapAsset`
  - `avm.createVariableCapAsset`
  - `avm.createNFTAsset`
  - `avm.createAddress`
  - `avm.listAddresses`
  - `avm.exportKey`
  - `avm.importKey`
  - `avm.mint`
  - `avm.sendNFT`
  - `avm.mintNFT`
  - `avm.import`
  - `avm.export`
  - `avm.send`
  - `avm.sendMultiple`
- Deprecated the `avm/wallet` API endpoint
  - `wallet.issueTx`
  - `wallet.send`
  - `wallet.sendMultiple`
- Deprecated various `platformvm` APIs
  - `platform.exportKey`
  - `platform.importKey`
  - `platform.getBalance`
  - `platform.createAddress`
  - `platform.listAddresses`
  - `platform.getSubnets`
  - `platform.addValidator`
  - `platform.addDelegator`
  - `platform.addSubnetValidator`
  - `platform.createSubnet`
  - `platform.exportAVAX`
  - `platform.importAVAX`
  - `platform.createBlockchain`
  - `platform.getBlockchains`
  - `platform.getStake`
  - `platform.getMaxStakeAmount`
  - `platform.getRewardUTXOs`
- Deprecated the `stake` field in the `platform.getTotalStake` response in favor of `weight`

## [v1.9.11](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.11)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

### Plugins

- Removed error from `logging.NoLog#Write`
- Added logging to the static VM factory usage
- Fixed incorrect error being returned from `subprocess.Bootstrap`

### Ledger

- Added ledger tx parsing support

### MerkleDB

- Added explicit consistency guarantees when committing multiple `merkledb.trieView`s to disk at once
- Removed reliance on premature root calculations for `merkledb.trieView` validity tracking
- Updated `x/merkledb/README.md`

## [v1.9.10](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.10)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

### MerkleDB

- Removed parent tracking from `merkledb.trieView`
- Removed `base` caches from `merkledb.trieView`
- Fixed error handling during `merkledb` intermediate node eviction
- Replaced values larger than `32` bytes with a hash in the `merkledb` hash representation

### AVM

- Refactored `avm` API tx creation into a standalone `Spender` implementation
- Migrated UTXO interfaces from the `platformvm` into the `components` for use in the `avm`
- Refactored `avm` `tx.SyntacticVerify` to expect the config rather than the fee fields

### Miscellaneous

- Updated the minimum golang version to `v1.19.6`
- Fixed `rpcchainvm` signal handling to only shutdown upon receipt of `SIGTERM`
- Added `warp.Signature#NumSigners` for better cost tracking support
- Added `snow.Context#PublicKey` to provide access to the local node's BLS public key inside the VM execution environment
- Renamed Avalanche consensus metric prefix to `avalanche_{chainID}_avalanche`
- Specified an explicit TCP `Linger` timeout of `15` seconds
- Updated the `secp256k1` library to `v4.1.0`

### Cleanup

- Removed support for the `--whitelisted-subnets` flag
- Removed unnecessary abstractions from the `app` package
- Removed `Factory` embedding from `platformvm.VM` and `avm.VM`
- Removed `validator` package from the `platformvm`
- Removed `timer.TimeoutManager`
- Replaced `snow.Context` in `Factory.New` with `logging.Logger`
- Renamed `set.Bits#Len` to `BitLen` and `set.Bits#HammingWeight` to `Len` to align with `set.Bits64`

## [v1.9.9](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.9)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `23`.

**Note: The `--whitelisted-subnets` flag was deprecated in `v1.9.6`. This is the last release in which it will be supported. Use `--track-subnets` instead.**

### Monitoring

- Added warning when the P2P server IP is private
- Added warning when the HTTP server IP is potentially publicly reachable
- Removed `merkledb.trieView#calculateIDs` tracing when no recalculation is needed

### Databases

- Capped the number of goroutines that `merkledb.trieView#calculateIDsConcurrent` will create
- Removed `nodb` package
- Refactored `Batch` implementations to share common code
- Added `Batch.Replay` invariant tests
- Converted to use `require` in all `database` interface tests

### Cryptography

- Moved the `secp256k1` implementations to a new `secp256k1` package out of the `crypto` package
- Added `rfc6979` compliance tests to the `secp256k1` signing implementation
- Removed unused cryptography implementations `ed25519`, `rsa`, and `rsapss`
- Removed unnecessary cryptography interfaces `crypto.Factory`, `crypto.RecoverableFactory`, `crypto.PublicKey`, and `crypto.PrivateKey`
- Added verification when parsing `secp256k1` public keys to ensure usage of the compressed format

### API

- Removed delegators from `platform.getCurrentValidators` unless a single `nodeID` is requested
- Added `delegatorCount` and `delegatorWeight` to the validators returned by `platform.getCurrentValidators`

### Documentation

- Improved documentation on the `block.WithVerifyContext` interface
- Fixed `--public-ip` and `--public-ip-resolution-service` CLI flag descriptions
- Updated `README.md` to explicitly reference `SECURITY.md`

### Coreth

- Enabled state sync by default when syncing from an empty database
- Increased block gas limit to 15M for `Cortina` Network Upgrade
- Added back file tracer endpoint
- Added back JS tracer

### Miscellaneous

- Added `allowedNodes` to the subnet config for `validatorOnly` subnets
- Removed the `hashicorp/go-plugin` dependency to improve plugin flexibility
- Replaced specialized `bag` implementations with generic `bag` implementations
- Added `mempool` package to the `avm`
- Added `chain.State#IsProcessing` to simplify integration with `block.WithVerifyContext`
- Added `StateSyncMinVersion` to `sync.ClientConfig`
- Added validity checks for `InitialStakeDuration` in a custom network genesis
- Removed unnecessary reflect call when marshalling an empty slice

### Cleanup

- Renamed `teleporter` package to `warp`
- Replaced `bool` flags in P-chain state diffs with an `enum`
- Refactored subnet configs to more closely align between the primary network and subnets
- Simplified the `utxo.Spender` interface
- Removed unused field `common.Config#Validators`

## [v1.9.8](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.8)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `22`.

### Networking

- Added TCP proxy support for p2p network traffic
- Added p2p network client utility for directly messaging the p2p network

### Consensus

- Guaranteed delivery of App messages to the VM, regardless of sync status
- Added `EngineType` to consensus context

### MerkleDB - Alpha

- Added initial implementation of a path-based merkle-radix tree
- Added initial implementation of state sync powered by the merkledb

### APIs

- Updated `platform.getCurrentValidators` to return `uptime` as a percentage
- Updated `platform.get*Validators` to avoid iterating over the staker set when requesting specific nodeIDs
- Cached staker data in `platform.get*Validators` to significantly reduce DB IO
- Added `stakeAmount` and `weight` to all staker responses in P-chain APIs
- Deprecated `stakeAmount` in staker responses from P-chain APIs
- Removed `creationTxFee` from `info.GetTxFeeResponse`
- Removed `address` from `platformvm.GetBalanceRequest`

### Fixes

- Fixed `RemoveSubnetValidatorTx` weight diff corruption
- Released network lock before attempting to close a peer connection
- Fixed X-Chain last accepted block initialization to use the genesis block, not the stop vertex after linearization
- Removed plugin directory handling from AMI generation
- Removed copy of plugins directory from tar script

### Cleanup

- Removed unused rpm packaging scripts
- Removed engine dependency from chain registrants
- Removed unused field from chain handler log
- Linted custom test `chains.Manager`
- Used generic btree implementation
- Deleted `utils.CopyBytes`
- Updated rjeczalik/notify from v0.9.2 to v0.9.3

### Miscellaneous

- Added AVM `state.Chain` interface
- Added generic atomic value utility
- Added test for the AMI builder during RCs
- Converted cache implementations to use generics
- Added optional cache eviction callback

## [v1.9.7](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.7)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `22`.

### Fixes

- Fixed subnet validator lookup regression

## [v1.9.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.6)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `22`.

### Consensus

- Added `StateSyncMode` to the return of `StateSummary#Accept` to support syncing chain state while tracking the chain as a light client
- Added `AcceptedFrontier` to `Chits` messages
- Reduced unnecessary confidence resets during consensus by applying `AcceptedFrontier`s during `QueryFailed` handling
- Added EngineType for consensus messages in the p2p message definitions
- Updated `vertex.DAGVM` interface to support linearization

### Configs

- Added `--plugin-dir` flag. The default value is `[DATADIR]/plugins`
- Removed `--build-dir` flag. The location of the avalanchego binary is no longer considered when looking for the `plugins` directory. Subnet maintainers should ensure that their node is able to properly discover plugins, as the default location is likely changed. See `--plugin-dir`
- Changed the default value of `--api-keystore-enabled` to `false`
- Added `--track-subnets` flag as a replacement of `--whitelisted-subnets`

### Fixes

- Fixed NAT-PMP router discovery and port mapping
- Fixed `--staking-enabled=false` setting to correctly start subnet chains and report healthy
- Fixed message logging in the consensus handler

### VMs

- Populated non-trivial logger in the `rpcchainvm` `Server`'s `snow.Context`
- Updated `rpcchainvm` proto definitions to use enums
- Added `Block` format and definition to the `AVM`
- Removed `proposervm` height index reset

### Metrics

- Added `avalanche_network_peer_connected_duration_average` metric
- Added `avalanche_api_calls_processing` metric
- Added `avalanche_api_calls` metric
- Added `avalanche_api_calls_duration` metric

### Documentation

- Added wallet example to create `stakeable.LockOut` outputs
- Improved ubuntu deb install instructions

### Miscellaneous

- Updated ledger-avalanche to v0.6.5
- Added linter to ban the usage of `fmt.Errorf` without format directives
- Added `List` to the `buffer#Deque` interface
- Added `Index` to the `buffer#Deque` interface
- Added `SetLevel` to the `Logger` interface
- Updated `auth` API to use the new `jwt` standard

## [v1.9.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.5)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `21`.

### Subnet Messaging

- Added subnet message serialization format
- Added subnet message signing
- Replaced `bls.SecretKey` with a `teleporter.Signer` in the `snow.Context`
- Moved `SNLookup` into the `validators.State` interface to support non-whitelisted chainID to subnetID lookups
- Added support for non-whitelisted subnetIDs for fetching the validator set at a given height
- Added subnet message verification
- Added `teleporter.AnycastID` to denote a subnet message not intended for a specific chain

### Fixes

- Added re-gossip of updated validator IPs
- Fixed `rpcchainvm.BatchedParseBlock` to correctly wrap returned blocks
- Removed incorrect `uintptr` handling in the generic codec
- Removed message latency tracking on messages being sent to itself

### Coreth

- Added support for eth_call over VM2VM messaging
- Added config flags for tx pool behavior

### Miscellaneous

- Added networking package README.md
- Removed pagination of large db messages over gRPC
- Added `Size` to the generic codec to reduce allocations
- Added `UnpackLimitedBytes` and `UnpackLimitedStr` to the manual packer
- Added SECURITY.md
- Exposed proposer list from the `proposervm`'s `Windower` interface
- Added health and bootstrapping client helpers that block until the node is healthy
- Moved bit sets from the `ids` package to the `set` package
- Added more wallet examples

## [v1.9.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.4)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `20`.

**This version modifies the db format. The db format is compatible with v1.9.3, but not v1.9.2 or earlier. After running a node with v1.9.4 attempting to run a node with a version earlier than v1.9.3 may report a fatal error on startup.**

### PeerList Gossip Optimization

- Added gossip tracking to the `peer` instance to only gossip new `IP`s to a connection
- Added `PeerListAck` message to report which `TxID`s provided by the `PeerList` message were tracked
- Added `TxID`s to the `PeerList` message to unique-ify nodeIDs across validation periods
- Added `TxID` mappings to the gossip tracker

### Validator Set Tracking

- Renamed `GetValidators` to `Get` on the `validators.Manager` interface
- Removed `Set`, `AddWeight`, `RemoveWeight`, and `Contains` from the `validators.Manager` interface
- Added `Add` to the `validators.Manager` interface
- Removed `Set` from the `validators.Set` interface
- Added `Add` and `Get` to the `validators.Set` interface
- Modified `validators.Set#Sample` to return `ids.NodeID` rather than `valdiators.Validator`
- Replaced the `validators.Validator` interface with a struct
- Added a `BLS` public key field to `validators.Validator`
- Added a `TxID` field to `validators.Validator`
- Improved and documented error handling within the `validators.Set` interface
- Added `BLS` public keys to the result of `GetValidatorSet`
- Added `BuildBlockWithContext` as an optional VM method to build blocks at a specific P-chain height
- Added `VerifyWithContext` as an optional block method to verify blocks at a specific P-chain height

### Uptime Tracking

- Added ConnectedSubnet message handling to the chain handler
- Added SubnetConnector interface and implemented it in the platformvm
- Added subnet uptimes to p2p `pong` messages
- Added subnet uptimes to `platform.getCurrentValidators`
- Added `subnetID` as an argument to `info.Uptime`

### Fixes

- Fixed incorrect context cancellation of escaped contexts from grpc servers
- Fixed race condition between API initialization and shutdown
- Fixed race condition between NAT traversal initialization and shutdown
- Fixed race condition during beacon connection tracking
- Added race detection to the E2E tests
- Added additional message and sender tests

### Coreth

- Improved header and logs caching using maximum accepted depth cache
- Added config option to perform database inspection on startup
- Added configurable transaction indexing to reduce disk usage
- Added special case to allow transactions using Nick's Method to bypass API level replay protection
- Added counter metrics for number of accepted/processed logs

### APIs

- Added indices to the return values of `GetLastAccepted` and `GetContainerByID` on the `indexer` API client
- Removed unnecessary locking from the `info` API

### Chain Data

- Added `ChainDataDir` to the `snow.Context` to allow blockchains to canonically access disk outside avalanchego's database
- Added `--chain-data-dir` as a CLI flag to specify the base directory for all `ChainDataDir`s

### Miscellaneous

- Removed `Version` from the `peer.Network` interface
- Removed `Pong` from the `peer.Network` interface
- Reduced memory allocations inside the system throttler
- Added `CChainID` to the `snow.Context`
- Converted all sorting to utilize generics
- Converted all set management to utilize generics

## [v1.9.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.3)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `19`.

### Tracing

- Added `context.Context` to all `VM` interface functions
- Added `context.Context` to the `validators.State` interface
- Added additional message fields to `tracedRouter#HandleInbound`
- Added `tracedVM` implementations for `block.ChainVM` and `vertex.DAGVM`
- Added `tracedState` implementation for `validators.State`
- Added `tracedHandler` implementation for `http.Handler`
- Added `tracedConsensus` implementations for `snowman.Consensus` and `avalanche.Consensus`

### Fixes

- Fixed incorrect `NodeID` used in registered `AppRequest` timeouts
- Fixed panic when calling `encdb#NewBatch` after `encdb#Close`
- Fixed panic when calling `prefixdb#NewBatch` after `prefixdb#Close`

### Configs

- Added `proposerMinBlockDelay` support to subnet configs
- Added `providedFlags` field to the `initializing node` for easily observing custom node configs
- Added `--chain-aliases-file` and `--chain-aliases-file-content` CLI flags
- Added `--proposervm-use-current-height` CLI flag

### Coreth

- Added metric for number of processed and accepted transactions
- Added wait for state sync goroutines to complete on shutdown
- Increased go-ethereum dependency to v1.10.26
- Increased soft cap on transaction size limits
- Added back isForkIncompatible checks for all existing forks
- Cleaned up Apricot Phase 6 code

### Linting

- Added `unused-receiver` linter
- Added `unused-parameter` linter
- Added `useless-break` linter
- Added `unhandled-error` linter
- Added `unexported-naming` linter
- Added `struct-tag` linter
- Added `bool-literal-in-expr` linter
- Added `early-return` linter
- Added `empty-lines` linter
- Added `error-lint` linter

### Testing

- Added `scripts/build_fuzz.sh` and initial fuzz tests
- Added additional `Fx` tests
- Added additional `messageQueue` tests
- Fixed `vmRegisterer` tests

### Documentation

- Documented `Database.Put` invariant for `nil` and empty slices
- Documented avalanchego's versioning scheme
- Improved `vm.proto` docs

### Miscellaneous

- Added peer gossip tracker
- Added `avalanche_P_vm_time_until_unstake` and `avalanche_P_vm_time_until_unstake_subnet` metrics
- Added `keychain.NewLedgerKeychainFromIndices`
- Removed usage of `Temporary` error handling after `listener#Accept`
- Removed `Parameters` from all `Consensus` interfaces
- Updated `avalanche-network-runner` to `v1.3.0`
- Added `ids.BigBitSet` to extend `ids.BitSet64` for arbitrarily large sets
- Added support for parsing future subnet uptime tracking data to the P-chain's state implementation
- Increased validator set cache size
- Added `avax.UTXOIDFromString` helper for managing `UTXOID`s more easily

## [v1.9.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.2)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `19`.

### Coreth

- Added trie clean cache journaling to disk to improve processing time after restart
- Fixed regression where a snapshot could be marked as stale by the async acceptor during block processing
- Added fine-grained block processing metrics

### RPCChainVM

- Added `validators.State` to the rpcchainvm server's `snow.Context`
- Added `rpcProtocolVersion` to the output of `info.getNodeVersion`
- Added `rpcchainvm` protocol version to the output of the `--version` flag
- Added `version.RPCChainVMProtocolCompatibility` map to easily compare plugin compatibility against avalanchego versions

### Builds

- Downgraded `ubuntu` release binaries from `jammy` to `focal`
- Updated macos github runners to `macos-12`
- Added workflow dispatch to build release binaries

### BLS

- Added bls proof of possession to `platform.getCurrentValidators` and `platform.getPendingValidators`
- Added bls public key to in-memory staker objects
- Improved memory clearing of bls secret keys

### Cleanup

- Fixed issue where the chain manager would attempt to start chain creation multiple times
- Fixed race that caused the P-chain to finish bootstrapping before the primary network finished bootstrapping
- Converted inbound message handling to expect usage of types rather than maps of fields
- Simplified the `validators.Set` implementation
- Added a warning if synchronous consensus messages take too long

## [v1.9.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.1)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `18`.

### Features

- Added cross-chain messaging support to the VM interface
- Added Ledger support to the Primary Network wallet
- Converted Bionic builds to Jammy builds
- Added `mock.gen.sh` to programmatically generate mock implementations
- Added BLS signer to the `snow.Context`
- Moved `base` from `rpc.NewEndpointRequester` to be included in the `method` in `SendRequest`
- Converted `UnboundedQueue` to `UnboundedDeque`

### Observability

- Added support for OpenTelemetry tracing
- Converted periodic bootstrapping status update to be time-based
- Removed duplicated fields from the json format of the node config
- Configured min connected stake health check based on the consensus parameters
- Added new consensus metrics
- Documented how chain time is advanced in the PlatformVM with `chain_time_update.md`

### Cleanup

- Converted chain creation to be handled asynchronously from the P-chain's execution environment
- Removed `SetLinger` usage of P2P TCP connections
- Removed `Banff` upgrade flow
- Fixed ProposerVM inner block caching after verification
- Fixed PlatformVM mempool verification to use an updated chain time
- Removed deprecated CLI flags: `--dynamic-update-duration`, `--dynamic-public-ip`
- Added unexpected Put bytes tests to the Avalanche and Snowman consensus engines
- Removed mockery generated mock implementations
- Converted safe math functions to use generics where possible
- Added linting to prevent usage of `assert` in unit tests
- Converted empty struct usage to `nil` for interface compliance checks
- Added CODEOWNERs to own first rounds of PR review

## [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)

This upgrade adds support for creating Proof-of-Stake Subnets.

This version is not backwards compatible. The changes in the upgrade go into effect at 12 PM EDT, October 18th 2022 on Mainnet.

**All Mainnet nodes should upgrade before 12 PM EDT, October 18th 2022.**

The supported plugin version is `17`.

### Upgrades

- Activated P2P serialization format change to Protobuf
- Activated non-AVAX `ImportTx`/`ExportTx`s to/from the P-chain
- Activated `Banff*` blocks on the P-chain
- Deactivated `Apricot*` blocks on the P-chain
- Activated `RemoveSubnetValidatorTx`s on the P-chain
- Activated `TransformSubnetTx`s on the P-chain
- Activated `AddPermissionlessValidatorTx`s on the P-chain
- Activated `AddPermissionlessDelegatorTx`s on the P-chain
- Deactivated ANT `ImportTx`/`ExportTx`s on the C-chain
- Deactivated ANT precompiles on the C-chain

### Deprecations

- Ubuntu 18.04 releases are deprecated and will not be provided for `>=v1.9.1`

### Miscellaneous

- Fixed locked input signing in the P-chain wallet
- Removed assertions from the logger interface
- Removed `--assertions-enabled` flag
- Fixed typo in `--bootstrap-max-time-get-ancestors` flag
- Standardized exported P-Chain codec usage
- Improved isolation and execution of the E2E tests
- Updated the linked hashmap implementation to use generics

## [v1.8.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.6)

This version is backwards compatible to [v1.8.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0). It is optional, but encouraged. The supported plugin version is `16`.

### BLS

- Added BLS key file at `--staking-signer-key-file`
- Exposed BLS proof of possession in the `info.getNodeID` API
- Added BLS proof of possession to `AddPermissionlessValidatorTx`s for the Primary Network

The default value of `--staking-signer-key-file` is `~/.avalanchego/staking/signer.key`. If the key file doesn't exist, it will be populated with a new key.

### Networking

- Added P2P proto support to be activated in a future release
- Fixed inbound bandwidth spike after leaving the validation set
- Removed support for `ChitsV2` messages
- Removed `ContainerID`s from `Put` and `PushQuery` messages
- Added `pending_timeouts` metric to track the number of active timeouts a node is tracking
- Fixed overflow in gzip decompression
- Optimized memory usage in `peer.MessageQueue`

### Miscellaneous

- Fixed bootstrapping ETA metric
- Removed unused `unknown_txs_count` metric
- Replaced duplicated code with generic implementations

### Coreth

- Added failure reason to bad block API

## [v1.8.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.5)

Please upgrade your node as soon as possible.

The supported plugin version is `16`.

### Fixes

- Fixed stale block reference by evicting blocks upon successful verification

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Removed check for Apricot Phase6 incompatible fork to unblock nodes that did not upgrade ahead of the activation time

## [v1.8.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.4)

Please upgrade your node as soon as possible.

The supported plugin version is `16`.

### Caching

- Added temporarily invalid block caching to reduce repeated network requests
- Added caching to the proposervm's inner block parsing

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Reduced the log level of `BAD BLOCK`s from `ERROR` to `DEBUG`
- Deprecated Native Asset Call

## [v1.8.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.2)

Please upgrade your node as soon as possible.

The changes in `v1.8.x` go into effect at 4 PM EDT on September 6th, 2022 on both Fuji and Mainnet. You should upgrade your node before the changes go into effect, otherwise they may experience loss of uptime.

The supported plugin version is `16`.

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Fixed live-lock in bootstrapping, after performing state-sync, by properly reporting `database.ErrNotFound` in `GetBlockIDAtHeight` rather than a formatted error
- Increased the log level of `BAD BLOCK`s from `DEBUG` to `ERROR`
- Fixed typo in Chain Config `String` function

## [v1.8.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.1)

Please upgrade your node as soon as possible.

The changes in `v1.8.x` go into effect at 4 PM EDT on September 6th, 2022 on both Fuji and Mainnet. You should upgrade your node before the changes go into effect, otherwise they may experience loss of uptime.

The supported plugin version is `16`.

### Miscellaneous

- Reduced the severity of not quickly connecting to bootstrap nodes from `FATAL` to `WARN`

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Reduced the log level of `BAD BLOCK`s from `ERROR` to `DEBUG`
- Added Apricot Phase6 to Chain Config `String` function

## [v1.8.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)

This is a mandatory security upgrade. Please upgrade your node **as soon as possible.**

The changes in the upgrade go into effect at **4 PM EDT on September 6th, 2022** on both Fuji and Mainnet. You should upgrade your node before the changes go into effect, otherwise they may experience loss of uptime.

You may see some extraneous ERROR logs ("BAD BLOCK") on your node after upgrading. These may continue until the Apricot Phase 6 activation (at 4 PM EDT on September 6th).

The supported plugin version is `16`.

### PlatformVM APIs

- Fixed `GetBlock` API when requesting the encoding as `json`
- Changed the json key in `AddSubnetValidatorTx`s from `subnet` to `subnetID`
- Added multiple asset support to `getBalance`
- Updated `PermissionlessValidator`s returned from `getCurrentValidators` and `getPendingValidators` to include `validationRewardOwner` and `delegationRewardOwner`
- Deprecated `rewardOwner` in `PermissionlessValidator`s returned from `getCurrentValidators` and `getPendingValidators`
- Added `subnetID` argument to `getCurrentSupply`
- Added multiple asset support to `getStake`
- Added `subnetID` argument to `getMinStake`

### PlatformVM Structures

- Renamed existing blocks
  - `ProposalBlock` -> `ApricotProposalBlock`
  - `AbortBlock` -> `ApricotAbortBlock`
  - `CommitBlock` -> `ApricotCommitBlock`
  - `StandardBlock` -> `ApricotStandardBlock`
  - `AtomicBlock` -> `ApricotAtomicBlock`
- Added new block types **to be enabled in a future release**
  - `BlueberryProposalBlock`
    - Introduces a `Time` field and an unused `Txs` field before the remaining `ApricotProposalBlock` fields
  - `BlueberryAbortBlock`
    - Introduces a `Time` field before the remaining `ApricotAbortBlock` fields
  - `BlueberryCommitBlock`
    - Introduces a `Time` field before the remaining `ApricotCommitBlock` fields
  - `BlueberryStandardBlock`
    - Introduces a `Time` field before the remaining `ApricotStandardBlock` fields
- Added new transaction types **to be enabled in a future release**
  - `RemoveSubnetValidatorTx`
    - Can be included into `BlueberryStandardBlock`s
    - Allows a subnet owner to remove a validator from their subnet
  - `TransformSubnetTx`
    - Can be included into `BlueberryStandardBlock`s
    - Allows a subnet owner to convert their subnet into a permissionless subnet
  - `AddPermissionlessValidatorTx`
    - Can be included into `BlueberryStandardBlock`s
    - Adds a new validator to the requested permissionless subnet
  - `AddPermissionlessDelegatorTx`
    - Can be included into `BlueberryStandardBlock`s
    - Adds a new delegator to the requested permissionless validator on the requested subnet

### PlatformVM Block Building

- Fixed race in `AdvanceTimeTx` creation to avoid unnecessary block construction
- Added `block_formation_logic.md` to describe how blocks are created
- Refactored `BlockBuilder` into `ApricotBlockBuilder`
- Added `BlueberryBlockBuilder`
- Added `OptionBlock` builder visitor
- Refactored `Mempool` issuance and removal logic to use transaction visitors

### PlatformVM Block Execution

- Added support for executing `AddValidatorTx`, `AddDelegatorTx`, and `AddSubnetValidatorTx` inside of a `BlueberryStandardBlock`
- Refactored time advancement into a standard state modification structure
- Refactored `ProposalTxExecutor` to abstract state diff creation
- Standardized upgrade checking rules
- Refactored subnet authorization checking

### Wallet

- Added support for new transaction types in the P-chain wallet
- Fixed fee amounts used in the Primary Network wallet to reduce unnecessary fee burning

### Networking

- Defined `p2p.proto` to be used for future network messages
- Added `--network-tls-key-log-file-unsafe` to support inspecting p2p messages
- Added `avalanche_network_accept_failed` metrics to track networking `Accept` errors

### Miscellaneous

- Removed reserved fields from proto files and renumbered the existing fields
- Added generic dynamically resized ring buffer
- Updated gRPC version to `v1.49.0` to fix non-deterministic errors reported in the `rpcchainvm`
- Removed `--signature-verification-enabled` flag
- Removed dead code
  - `ids.QueueSet`
  - `timer.Repeater`
  - `timer.NewStagedTimer`
  - `timer.TimedMeter`

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Incorrectly deprecated Native Asset Call
- Migrated to go-ethereum v1.10.23
- Added API to fetch Chain Config

## [v1.7.18](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.18)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### Fixes

- Fixed bug in `codeToFetch` database accessors that caused an error when starting/stopping state sync
- Fixed rare BAD BLOCK errors during C-chain bootstrapping
- Fixed platformvm `couldn't get preferred block state` log due to attempted block building during bootstrapping
- Fixed platformvm `failed to fetch next staker to reward` error log due to an incorrect `lastAcceptedID` reference
- Fixed AWS AMI creation

### PlatformVM

- Refactored platformvm metrics handling
- Refactored platformvm block creation
- Introduced support to prevent empty nodeID use on the P-chain to be activated in a future upgrade

### Coreth

- Updated gas price estimation to limit lookback window based on block timestamps
- Added metrics for processed/accepted gas
- Simplified syntactic block verification
- Ensured statedb errors during block processing are logged
- Removed deprecated gossiper/block building logic from pre-Apricot Phase 4
- Added marshal function for duration to improve config output

### Miscellaneous

- Updated local network genesis to use a newer start time
- Updated minimum golang version to go1.18.1
- Removed support for RocksDB
- Bumped go-ethereum version to v1.10.21
- Added various additional tests
- Introduced additional database invariants for all database implementations
- Added retries to windows CI installations
- Removed useless ID aliasing during chain creation

## [v1.7.17](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.17)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### VMs

- Refactored P-chain block state management
  - Supporting easier parsing and usage of blocks
  - Improving separation of block execution with block definition
  - Unifying state definitions
- Introduced support to send custom X-chain assets to the P-chain to be activated in a future upgrade
- Introduced support to use custom assets on the P-chain to be activated in a future upgrade
- Added VMs README to begin fully documenting plugin invariants
- Added various comments around expected usages of VM tools

### Coreth

- Added optional JSON logging
- Added interface for supporting stateful precompiles
- Removed legacy code format from the database

### Fixes

- Fixed ungraceful gRPC connection closure during very long running requests
- Fixed LevelDB panic during shutdown
- Fixed verification of `--stake-max-consumption-rate` to include the upper-bound
- Fixed various CI failures
- Fixed flaky unit tests

### Miscellaneous

- Added bootstrapping ETA metrics
- Converted all logs to support structured fields
- Improved Snowman++ oracle block verification error messages
- Removed deprecated or unused scripts

## [v1.7.16](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.16)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### LevelDB

- Fix rapid disk growth by manually specifying the maximum manifest file size

## [v1.7.15](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.15)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### PlatformVM

- Replaced copy-on-write validator set data-structure to use tree diffs to optimize validator set additions
- Replaced validation transactions with a standardized representation to remove transaction type handling
- Migrated transaction execution to its own package
- Removed child pointers from processing blocks
- Added P-chain wallet helper for providing initial transactions

### Coreth

- Bumped go-ethereum dependency to v1.10.20
- Updated API names used to enable services in `eth-api` config flag. Prior names are supported but deprecated, please update configurations [accordingly](https://docs.avax.network/nodes/maintain/chain-config-flags#c-chain-configs)
- Optimized state sync by parallelizing trie syncing
- Added `eth_syncing` API for compatibility. Note: This API is only accessible after bootstrapping and always returns `"false"`, since the node will no longer be syncing at that point
- Added metrics to the atomic transaction mempool
- Added metrics for incoming/outgoing mempool gossip

### Fixes

- Updated Snowman and Avalanche consensus engines to report original container preferences before processing the provided container
- Fixed inbound message byte throttler context cancellation cleanup
- Removed case sensitivity of IP resolver services
- Added failing health check when a whitelisted subnet fails to initialize a chain

### Miscellaneous

- Added gRPC client metrics for dynamically created connections
- Added uninitialized continuous time averager for when initial predictions are unreliable
- Updated linter version
- Documented various platform invariants
- Cleaned up various dead parameters
- Improved various tests

## [v1.7.14](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.14)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### APIs

**These API format changes are breaking changes. https://api.avax.network and https://api.avax-test.network have been updated with this format. If you are using AvalancheGo APIs in your code, please ensure you have updated to the latest versions. See  https://docs.avax.network/apis/avalanchego/cb58-deprecation for details about the CB58 removal.**

- Removed `CB58` as an encoding option from all APIs
- Added `HexC` and `HexNC` as encoding options for all APIs that accept an encoding format
- Removed the `Success` response from all APIs
- Replaced `containerID` with `id` in the indexer API

### PlatformVM

- Fixed incorrect `P-chain` height in `Snowman++` when staking is disabled
- Moved `platformvm` transactions to be defined in a sub-package
- Moved `platformvm` genesis management to be defined in a sub-package
- Moved `platformvm` state to be defined in a sub-package
- Standardized `platformvm` transactions to always be referenced via pointer
- Moved the `platformvm` transaction builder to be defined in a sub-package
- Fixed uptime rounding during node shutdown

### Coreth

- Bumped go-ethereum dependency to v1.10.18
- Parallelized state sync code fetching

### Networking

- Updated `Connected` and `Disconnected` messages to only be sent to chains if the peer is tracking the subnet
- Updated the minimum TLS version on the p2p network to `v1.3`
- Supported context cancellation in the networking rate limiters
- Added `ChitsV2` message format for the p2p network to be used in a future upgrade

### Miscellaneous

- Fixed `--public-ip-resolution-frequency` invalid overwrite of the resolution service
- Added additional metrics to distinguish between virtuous and rogue currently processing transactions
- Suppressed the super cool `avalanchego` banner when `stdout` is not directed to a terminal
- Updated linter version
- Improved various comments and documentation
- Standardized primary network handling across subnet maps

## [v1.7.13](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.13)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### State Sync

- Added peer bandwidth tracking to optimize `coreth` state sync message routing
- Fixed `coreth` leaf request handler bug to ensure the handler delivers a valid range proof
- Removed redundant proof keys from `coreth` leafs response message format
- Improved `coreth` state sync request retry logic
- Improved `coreth` state sync handler metrics
- Improved `coreth` state sync ETA
- Added `avalanche_{chainID}_handler_async_expired` metric

### Miscellaneous

- Fixed `platform.getCurrentValidators` API to correctly mark a node as connected to itself on subnets.
- Fixed `platform.getBlockchainStatus` to correctly report `Unknown` for blockchains that are not managed by the `P-Chain`
- Added process metrics by default in the `rpcchainvm#Server`
- Added `Database` health checks
- Removed the deprecated `Database.Stat` call from the `rpcdb#Server`
- Added fail fast logic to duplicated Snowman additions to avoid undefined behavior
- Added additional testing around Snowman diverged voting tests
- Deprecated `--dynamic-update-duration` and `--dynamic-public-ip` CLI flags
- Added `--public-ip-resolution-frequency` and `--public-ip-resolution-service` to replace `--dynamic-update-duration` and `--dynamic-public-ip`, respectively

## [v1.7.12](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.12)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### State Sync

- Fixed proposervm state summary acceptance to only accept state summaries with heights higher than the locally last accepted block
- Fixed proposervm state summary serving to only respond to requests after height indexing has finished
- Improved C-chain state sync leaf request serving by optimistically reading leaves from snapshot
- Refactored C-chain state sync block fetching

### Networking

- Reduced default peerlist and accepted frontier gossiping
- Increased the default at-large outbound buffer size to 32 MiB

### Metrics

- Added leveldb metrics
- Added process and golang metrics for the avalanchego binary
- Added available disk space health check
  - Ensured that the disk space will not be fully utilized by shutting down the node if there is a critically low amount of free space remaining
- Improved C-chain state sync metrics

### Performance

- Added C-chain acceptor queue within `core/blockchain.go`
- Removed rpcdb locking when committing batches and using iterators
- Capped C-chain TrieDB dirties cache size during block acceptance to reduce commit size at 4096 block interval

### Cleanup

- Refactored the avm to utilize the external txs package
- Unified platformvm dropped tx handling
- Clarified snowman child block acceptance calls
- Fixed small consensus typos
- Reduced minor duplicated code in consensus
- Moved the platformvm key factory out of the VM into the test file
- Removed unused return values from the timeout manager
- Removed weird json rpc private interface
- Standardized json imports
- Added vm factory interface checks

## [v1.7.11](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.11)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

**The first startup of the C-Chain will cause an increase in CPU and IO usage due to an index update. This index update runs in the background and does not impact restart time.**

### State Sync

- Added state syncer engine to facilitate VM state syncing, rather than full historical syncing
- Added `GetStateSummaryFrontier`, `StateSummaryFrontier`, `GetAcceptedStateSummary`, `AcceptedStateSummary` as P2P messages
- Updated `Ancestors` message specification to expect an empty response if the container is unknown
- Added `--state-sync-ips` and `--state-sync-ids` flags to allow manual overrides of which nodes to query for accepted state summaries
- Updated networking library to permanently track all manually tracked peers, rather than just beacons
- Added state sync support to the `metervm`
- Added state sync support to the `proposervm`
- Added state sync support to the `rpcchainvm`
- Added beta state sync support to `coreth`

### ProposerVM

- Prevented rejected blocks from overwriting the `proposervm` height index
- Optimized `proposervm` block rewind to utilize the height index if available
- Ensured `proposervm` height index is marked as repaired in `Initialize` if it is fully repaired on startup
- Removed `--reset-proposervm-height-index`. The height index will be reset upon first restart
- Optimized `proposervm` height index resetting to periodically flush deletions

### Bug Fixes

- Fixed IPC message issuance and restructured consensus event callbacks to be checked at compile time
- Fixed `coreth` metrics initialization
- Fixed bootstrapping startup logic to correctly startup if initially connected to enough stake
- Fixed `coreth` panic during metrics collection
- Fixed panic on concurrent map read/write in P-chain wallet SDK
- Fixed `rpcchainvm` panic by sanitizing http response codes
- Fixed incorrect JSON tag on `platformvm.BaseTx`
- Fixed `AppRequest`, `AppResponse`, and `AppGossip` stringers used in logging

### API/Client

- Supported client implementations pointing to non-standard URIs
- Introduced `ids.NodeID` type to standardize logging and simplify API service and client implementations
- Changed client implementations to use standard types rather than `string`s wherever possible
- Added `subnetID` as an argument to `platform.getTotalStake`
- Added `connected` to the subnet validators in responses to `platform.getCurrentValidators` and `platform.getPendingValidators`
- Add missing `admin` API client methods
- Improved `indexer` API client implementation to avoid encoding edge cases

### Networking

- Added `--snow-mixed-query-num-push-vdr` and `--snow-mixed-query-num-push-non-vdr` to allow parameterization of sending push queries
  - By default, non-validators now send only pull queries, not push queries.
  - By default, validators now send both pull queries and push queries upon inserting a container into consensus. Previously, nodes sent only push queries.
- Added metrics to track the amount of over gossiping of `peerlist` messages
- Added custom message queueing support to outbound `Peer` messages
- Reused `Ping` messages to avoid needless memory allocations

### Logging

- Replaced AvalancheGo's internal logger with [uber-go/zap](https://github.com/uber-go/zap).
- Replaced AvalancheGo's log rotation with [lumberjack](https://github.com/natefinch/lumberjack).
- Renamed `log-display-highlight` to `log-format` and added `json` option.
- Added `log-rotater-max-size`, `log-rotater-max-files`, `log-rotater-max-age`, `log-rotater-compress-enabled` options for log rotation.

### Miscellaneous

- Added `--data-dir` flag to easily move all default file locations to a custom location
- Standardized RPC specification of timestamp fields
- Logged health checks whenever a failing health check is queried
- Added callback support for the validator set manager
- Increased `coreth` trie tip buffer size to 32
- Added CPU usage metrics for AvalancheGo and all sub-processes
- Added Disk IO usage metrics for AvalancheGo and all sub-processes

### Cleanup

- Refactored easily separable `platformvm` files into separate smaller packages
- Simplified default version parsing
- Fixed various typos
- Converted some structs to interfaces to better support mocked testing
- Refactored IP utils

### Documentation

- Increased recommended disk size to 1 TB
- Updated issue template
- Documented additional `snowman.Block` invariants

## [v1.7.10](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.10)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Networking

- Improved vertex and block gossiping for validators with low stake weight.
- Added peers metric by subnet.
- Added percentage of stake connected metric by subnet.

### APIs

- Added support for specifying additional headers and query params in the RPC client implementations.
- Added static API clients for the `platformvm` and the `avm`.

### PlatformVM

- Introduced time based windowing of accepted P-chain block heights to ensure that local networks update the proposer list timely in the `proposervm`.
- Improved selection of decision transactions from the mempool.

### RPCChainVM

- Increased `buf` version to `v1.3.1`.
- Migrated all proto definitions to a dedicated `/proto` folder.
- Removed the dependency on the non-standard grpc broker to better support other language implementations.
- Added grpc metrics.
- Added grpc server health checks.

### Coreth

- Fixed a bug where a deadlock on shutdown caused historical re-generation on restart.
- Added an API endpoint to fetch the current VM Config.
- Added AvalancheGo custom log formatting to the logs.
- Removed support for the JS Tracer.

### Logging

- Added piping of subnet logs to stdout.
- Lazily initialized logs to avoid opening files that are never written to.
- Added support for arbitrarily deleted log files while avalanchego is running.
- Removed redundant logging configs.

### Miscellaneous

- Updated minimum go version to `v1.17.9`.
- Added subnet bootstrapping health checks.
- Supported multiple tags per codec instantiation.
- Added minor fail-fast optimization to string packing.
- Removed dead code.
- Fixed typos.
- Simplified consensus engine `Shutdown` notification dispatching.
- Removed `Sleep` call in the inbound connection throttler.

## [v1.7.9](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.9)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Updates

- Improved subnet gossip to only send messages to nodes participating in that subnet.
- Fixed inlined VM initialization to correctly register static APIs.
- Added logging for file descriptor limit errors.
- Removed dead code from network packer.
- Improved logging of invalid hash length errors.

## [v1.7.8](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.8)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Networking

- Fixed duplicate reference decrease when closing a peer.
- Freed allocated message buffers immediately after sending.
- Added `--network-peer-read-buffer-size` and `--network-peer-write-buffer-size` config options.
- Moved peer IP signature verification to enable concurrent verifications.
- Reduced the number of connection flushes when sending messages.
- Canceled outbound connection requests on shutdown.
- Reused dialer across multiple outbound connections.
- Exported `NewTestNetwork` for easier external testing.

### Coreth

- Reduced log level of snapshot regeneration logs.
- Enabled atomic tx replacement with higher gas fees.
- Parallelized trie index re-generation.

### Miscellaneous

- Fixed incorrect `BlockchainID` usage in the X-chain `ImportTx` builder.
- Fixed incorrect `OutputOwners` in the P-chain `ImportTx` builder.
- Improved FD limit error logging and warnings.
- Rounded bootstrapping ETAs to the nearest second.
- Added gossip config support to the subnet configs.
- Optimized various queue removals for improved memory freeing.
- Added a basic X-chain E2E usage test to the new testing framework.

## [v1.7.7](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.7)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Networking

- Refactored the networking library to track potential peers by nodeID rather than IP.
- Separated peer connections from the mesh network implementation to simplify testing.
- Fixed duplicate `Connected` messages bug.
- Supported establishing outbound connections with peers reporting different inbound and outbound IPs.

### Database

- Disabled seek compaction in leveldb by default.

### GRPC

- Increased protocol version, this requires all plugin definitions to update their communication dependencies.
- Merged services to be served using the same server when possible.
- Implemented a fast path for simple HTTP requests.
- Removed duplicated message definitions.
- Improved error reporting around invalid plugins.

### Coreth

- Optimized FeeHistory API.
- Added protection to prevent accidental corruption of archival node trie index.
- Added capability to restore complete trie index on best effort basis.
- Rounded up fastcache sizes to utilize all mmap'd memory in chunks of 64MB.

### Configs

- Removed `--inbound-connection-throttling-max-recent`
- Renamed `--network-peer-list-size` to `--network-peer-list-num-validator-ips`
- Removed `--network-peer-list-gossip-size`
- Removed `--network-peer-list-staker-gossip-fraction`
- Added `--network-peer-list-validator-gossip-size`
- Added `--network-peer-list-non-validator-gossip-size`
- Removed `--network-get-version-timeout`
- Removed `--benchlist-peer-summary-enabled`
- Removed `--peer-alias-timeout`

### Miscellaneous

- Fixed error reporting when making Avalanche chains that did not manually specify a primary alias.
- Added beacon utils for easier programmatic handling of beacon nodes.
- Resolved the default log directory on initialization to avoid additional error handling.
- Added support to the chain state module to specify an arbitrary new accepted block.

## [v1.7.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.6)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Consensus

- Introduced a new vertex type to support future `Avalanche` based network upgrades.
- Added pending message metrics to the chain message queues.
- Refactored event dispatchers to simplify dependencies and remove dead code.

### PlatformVM

- Added `json` encoding option to the `platform.getTx` call.
- Added `platform.getBlock` API.
- Cleaned up block building logic to be more modular and testable.

### Coreth

- Increased `FeeHistory` maximum historical limit to improve MetaMask UI on the C-Chain.
- Enabled chain state metrics.
- Migrated go-ethereum v1.10.16 changes.

### Miscellaneous

- Added the ability to load new VM plugins dynamically.
- Implemented X-chain + P-chain wallet that can be used to build and sign transactions. Without providing a full node private keys.
- Integrated e2e testing to the repo to avoid maintaining multiple synced repos.
- Fixed `proposervm` height indexing check to correctly mark the indexer as repaired.
- Introduced message throttling overrides to be used in future improvements to reliably send messages.
- Introduced a cap on the client specified request deadline.
- Increased the default `leveldb` open files limit to `1024`.
- Documented the `leveldb` configurations.
- Extended chain shutdown timeout.
- Performed various cleanup passes.

## [v1.7.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.5)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Consensus

- Added asynchronous processing of `App.*` messages.
- Added height indexing support to the `proposervm` and `rpcchainvm`. If a node is updated to `>=v1.7.5` and then downgraded to `<v1.7.5`, the user must enable the `--reset-proposervm-height-index=true` flag to ensure the `proposervm` height index is correctly updated going forward.
- Fixed bootstrapping job counter initialization that could cause negative ETAs to be reported.
- Fixed incorrect processing check that could log incorrect information.
- Removed incorrect warning logs.

### Miscellaneous

- Added tracked subnets to be reported in calls to the `info.peers` API.
- Updated gRPC implementations to use `buf` tooling and standardized naming and locations.
- Added a consistent hashing implementation to be used in future improvements.
- Fixed database iteration invariants to report `ErrClosed` rather than silently exiting.
- Added additional sanity checks to prevent users from incorrectly configuring their node.
- Updated log timestamps to include milliseconds.

### Coreth

- Added beta support for offline pruning.
- Refactored peer networking layer.
- Enabled cheap metrics by default.
- Marked RPC call metrics as expensive.
- Added Abigen support for native asset call precompile.
- Fixed bug in BLOCKHASH opcode during traceBlock.
- Fixed bug in handling updated chain config on startup.

## [v1.7.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.4)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

**The first startup of the C-Chain will take a few minutes longer due to an index update.**

### Consensus

- Removed deprecated Snowstorm consensus implementation that no longer aligned with the updated specification.
- Updated bootstrapping logs to no longer reset counters after a node restart.
- Added bootstrapping ETAs for fetching Snowman blocks and executing operations.
- Renamed the `MultiPut` message to the `Ancestors` message to match other message naming conventions.
- Introduced Whitelist conflicts into the Snowstorm specification that will be used in future X-chain improvements.
- Refactored the separation between the Bootstrapping engine and the Consensus engine to support Fast-Sync.

### Coreth

- Added an index mapping height to the list of accepted atomic operations at that height in a trie. Generating this index will cause the node to take a few minutes longer to startup the C-Chain for the first restart.
- Updated Geth dependency to `v1.10.15`.
- Updated `networkID` to match `chainID`.

### VMs

- Refactored `platformvm` rewards calculations to enable usage from an external library.
- Fixed `platformvm` and `avm` UTXO fetching to not re-iterate the UTXO set if no UTXOs are fetched.
- Refactored `platformvm` status definitions.
- Added support for multiple address balance lookups in the `platformvm`.
- Refactored `platformvm` and `avm` keystore users to reuse similar code.

### RPCChainVM

- Returned a `500 InternalServerError` if an unexpected gRPC error occurs during the handling of an HTTP request to a plugin.
- Updated gRPC server's max message size to enable responses larger than 4MiB from the plugin's handling of an HTTP request.

### Configs

- Added `--stake-max-consumption-rate` which defaults to `120,000`.
- Added `--stake-min-consumption-rate` which defaults to `100,000`.
- Added `--stake-supply-cap` which defaults to `720,000,000,000,000,000` nAVAX.
- Renamed `--bootstrap-multiput-max-containers-sent` to `--bootstrap-ancestors-max-containers-sent`.
- Renamed `--bootstrap-multiput-max-containers-received` to `--bootstrap-ancestors-max-containers-received`.
- Enforced that `--staking-enabled=false` can not be specified on public networks (`Fuji` and `Mainnet`).

### Metrics

- All `multi_put` metrics were converted to `ancestors` metrics.

### Miscellaneous

- Improved `corruptabledb` error reporting by tracking the first reported error.
- Updated CPU tracking to use the proper EWMA tracker rather than a linear approximation.
- Separated health checks into `readiness`, `healthiness`, and `liveness` checks to support more fine-grained monitoring.
- Refactored API client utilities to use a `Context` rather than an explicit timeout.

## [v1.7.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.3)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Consensus

- Introduced a notion of vertex conflicts that will be used in future X-chain improvements.

### Coreth

- Added an index mapping height to the list of accepted atomic transactions at that height. Generating this index will cause the node to take approximately 2 minutes longer to startup the C-Chain for the first restart.
- Fixed bug in base fee estimation API that impacted custom defined networks.
- Decreased minimum transaction re-gossiping interval from 1s to 500ms.
- Removed websocket handler from the static vm APIs.

### Database

- Reduced lock contention in `prefixDB`s.

### Networking

- Increase the gossip size from `6` to `10` validators.
- Prioritized `Connected` and `Disconnected` messages in the message handler.

### Miscellaneous

- Notified VMs of peer versions on `Connected`.
- Fixed acceptance broadcasting over IPC.
- Fixed 32-bit architecture builds for AvalancheGo (not Coreth).

## [v1.7.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.2)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Coreth

- Fixed memory leak in the estimate gas API.
- Reduced the default RPC gas limit to 50,000,000 gas.
- Improved RPC logging.
- Removed pre-AP5 legacy code.

### PlatformVM

- Optimized validator set change calculations.
- Removed storage of non-decided blocks.
- Simplified error handling.
- Removed pre-AP5 legacy code.

### Networking

- Explicitly fail requests with responses that failed to be parsed.
- Removed pre-AP5 legacy code.

### Configs

- Introduced the ability for a delayed graceful node shutdown.
- Added the ability to take all configs as environment variables for containerized deployments.

### Utils

- Fixed panic bug in logging library when importing from external projects.

## [v1.7.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.1)

This update is backwards compatible with [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). Please see the expected update times in the v1.7.0 release.

### Coreth

- Reduced fee estimate volatility.

### Consensus

- Fixed vote bubbling for unverified block chits.

## [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)

This upgrade adds support for issuing multiple atomic transactions into a single block and directly transferring assets between the P-chain and the C-chain.

The changes in the upgrade go into effect at 1 PM EST, December 2nd 2021 on Mainnet. One should upgrade their node before the changes go into effect, otherwise they may experience loss of uptime.

**All nodes should upgrade before 1 PM EST, December 2nd 2021.**

### Networking

- Added peer uptime reports as metrics.
- Removed IP rate limiting over local networks.

### PlatformVM

- Enabled `AtomicTx`s to be issued into `StandardBlock`s and deprecated `AtomicBlock`s.
- Added the ability to export/import AVAX to/from the C-chain.

### Coreth

- Enabled multiple `AtomicTx`s to be issued per block.
- Added the ability to export/import AVAX to/from the P-chain.
- Updated dynamic fee calculations.

### ProposerVM

- Removed storage of undecided blocks.

### RPCChainVM

- Added support for metrics to be reported by plugin VMs.

### Configs

- Removed `--snow-epoch-first-transition` and `snow-epoch-duration` as command line arguments.

## [v1.6.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.5)

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Bootstrapping

- Drop inbound messages to a chain if that chain is in the execution phase of bootstrapping.
- Print beacon nodeIDs upon failure to connect to them.

### Metrics

- Added `avalanche_{ChainID}_bootstrap_finished`, which is 1 if the chain is done bootstrapping, 0 otherwise.

### APIs

- Added `info.uptime` API call that attempts to report the network's view of the local node.
- Added `observedUptime` to each peer's result in `info.peers`.

### Network

- Added reported uptime to pong messages to be able to better track a local node's uptime as viewed by the network.
- Refactored request timeout registry to avoid a potential race condition.

## [v1.6.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.4)

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Config

- Added flag `throttler-inbound-bandwidth-refill-rate`, which specifies the max average inbound bandwidth usage of a peer.
- Added flag `throttler-inbound-bandwidth-max-burst-size`, which specifies the max inbound bandwidth usage of a peer.

### Networking

- Updated peerlist gossiping to use the same mechanism as other gossip calls.
- Added inbound message throttling based on recent bandwidth usage.

### Metrics

- Updated `avalanche_{ChainID}_handler_gossip_{count,sum}` to `avalanche_{ChainID}_handler_gossip_request_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_accepted_{count,sum}` to `avalanche_{ChainID}_lat_accepted_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_accepted_frontier_{count,sum}` to `avalanche_{ChainID}_lat_accepted_frontier_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_ancestors_{count,sum}` to `avalanche_{ChainID}_lat_multi_put_{count,sum}`.
- Combined `avalanche_{ChainID}_lat_pull_query_{count,sum}` and `avalanche_{ChainID}_lat_push_query_{count,sum}` to `avalanche_{ChainID}_lat_chits_{count,sum}`.
- Added `avalanche_{ChainID}_app_response_{count,sum}`.
- Added `avalanche_network_bandwidth_throttler_inbound_acquire_latency_{count,sum}`
- Added `avalanche_network_bandwidth_throttler_inbound_awaiting_acquire`
- Added `avalanche_P_vm_votes_won`
- Added `avalanche_P_vm_votes_lost`

### Indexer

- Added method `GetContainerByID` to client implementation.
- Client methods now return `[]byte` rather than `string` representations of a container.

### C-Chain

- Updated Geth dependency to 1.10.11.
- Added a new admin API for updating the log level and measuring performance.
- Added a new `--allow-unprotected-txs` flag to allow issuance of transactions without EIP-155 replay protection.

### Subnet & Custom VMs

- Ensured that all possible chains are run in `--staking-enabled=false` networks.

---

## [v1.6.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.3)

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Config Options

- Updated the default value of `--inbound-connection-throttling-max-conns-per-sec` to `256`.
- Updated the default value of `--meter-vms-enabled` to `true`.
- Updated the default value of `--staking-disabled-weight` to `100`.

### Metrics

- Changed the behavior of `avalanche_network_buffer_throttler_inbound_awaiting_acquire` to only increment if the message is actually blocking.
- Changed the behavior of `avalanche_network_byte_throttler_inbound_awaiting_acquire` to only increment if the message is actually blocking.
- Added `Block/Tx` metrics on `meterVM`s.
  - Added `avalanche_{ChainID}_vm_metervm_build_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_parse_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_get_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_verify_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_verify_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_accept_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_reject_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_parse_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_get_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_verify_tx_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_verify_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_accept_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_reject_{count,sum}`.

### Coreth

- Applied callTracer fault handling fix.
- Initialized multicoin functions in the runtime environment.

### ProposerVM

- Updated block `Delay` in `--staking-enabled=false` networks to be `0`.

## Historical Note

In the past, [subnet-evm](./graft/subnet-evm) and [coreth](./graft/coreth) were their own repositories. Their change logs are preserved for posterity here:

- coreth [RELEASES.MD](https://github.com/ava-labs/coreth/blob/master/RELEASES.md)
- subnet-evm [RELEASES.MD](https://github.com/ava-labs/subnet-evm/blob/master/RELEASES.md)
