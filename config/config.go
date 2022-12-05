// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/app/runner"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/storage"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/proposervm"
)

const (
	pluginsDirName       = "plugins"
	chainConfigFileName  = "config"
	chainUpgradeFileName = "upgrade"
	subnetConfigFileExt  = ".json"
)

var (
	deprecatedKeys = map[string]string{}

	errInvalidStakerWeights          = errors.New("staking weights must be positive")
	errStakingDisableOnPublicNetwork = errors.New("staking disabled on public network")
	errAuthPasswordTooWeak           = errors.New("API auth password is not strong enough")
	errInvalidUptimeRequirement      = errors.New("uptime requirement must be in the range [0, 1]")
	errMinValidatorStakeAboveMax     = errors.New("minimum validator stake can't be greater than maximum validator stake")
	errInvalidDelegationFee          = errors.New("delegation fee must be in the range [0, 1,000,000]")
	errInvalidMinStakeDuration       = errors.New("min stake duration must be > 0")
	errMinStakeDurationAboveMax      = errors.New("max stake duration can't be less than min stake duration")
	errStakeMaxConsumptionTooLarge   = fmt.Errorf("max stake consumption must be less than or equal to %d", reward.PercentDenominator)
	errStakeMaxConsumptionBelowMin   = errors.New("stake max consumption can't be less than min stake consumption")
	errStakeMintingPeriodBelowMin    = errors.New("stake minting period can't be less than max stake duration")
	errCannotWhitelistPrimaryNetwork = errors.New("cannot whitelist primary network")
	errStakingKeyContentUnset        = fmt.Errorf("%s key not set but %s set", StakingTLSKeyContentKey, StakingCertContentKey)
	errStakingCertContentUnset       = fmt.Errorf("%s key set but %s not set", StakingTLSKeyContentKey, StakingCertContentKey)
	errTracingEndpointEmpty          = fmt.Errorf("%s cannot be empty", TracingEndpointKey)
)

func GetRunnerConfig(v *viper.Viper) (runner.Config, error) {
	config := runner.Config{
		DisplayVersionAndExit: v.GetBool(VersionKey),
		BuildDir:              GetExpandedArg(v, BuildDirKey),
		PluginMode:            v.GetBool(PluginModeKey),
	}

	// Build directory should have this structure:
	//
	// build
	// ├── avalanchego (the binary from compiling the app directory)
	// └── plugins
	//     └── evm
	validBuildDir := func(dir string) bool {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return false
		}

		// make sure the expected subdirectory exists
		_, err = os.Stat(filepath.Join(dir, pluginsDirName))
		return err == nil
	}
	if validBuildDir(config.BuildDir) {
		return config, nil
	}

	foundBuildDir := false
	for _, dir := range defaultBuildDirs {
		dir = GetExpandedString(v, dir)
		if validBuildDir(dir) {
			config.BuildDir = dir
			foundBuildDir = true
			break
		}
	}
	if !foundBuildDir {
		return runner.Config{}, fmt.Errorf(
			"couldn't find valid build directory in any of the default locations: %s",
			defaultBuildDirs,
		)
	}
	return config, nil
}

func getConsensusConfig(v *viper.Viper) avalanche.Parameters {
	return avalanche.Parameters{
		Parameters: snowball.Parameters{
			K:                       v.GetInt(SnowSampleSizeKey),
			Alpha:                   v.GetInt(SnowQuorumSizeKey),
			BetaVirtuous:            v.GetInt(SnowVirtuousCommitThresholdKey),
			BetaRogue:               v.GetInt(SnowRogueCommitThresholdKey),
			ConcurrentRepolls:       v.GetInt(SnowConcurrentRepollsKey),
			OptimalProcessing:       v.GetInt(SnowOptimalProcessingKey),
			MaxOutstandingItems:     v.GetInt(SnowMaxProcessingKey),
			MaxItemProcessingTime:   v.GetDuration(SnowMaxTimeProcessingKey),
			MixedQueryNumPushVdr:    int(v.GetUint(SnowMixedQueryNumPushVdrKey)),
			MixedQueryNumPushNonVdr: int(v.GetUint(SnowMixedQueryNumPushNonVdrKey)),
		},
		BatchSize: v.GetInt(SnowAvalancheBatchSizeKey),
		Parents:   v.GetInt(SnowAvalancheNumParentsKey),
	}
}

func getLoggingConfig(v *viper.Viper) (logging.Config, error) {
	loggingConfig := logging.Config{}
	loggingConfig.Directory = GetExpandedArg(v, LogsDirKey)
	var err error
	loggingConfig.LogLevel, err = logging.ToLevel(v.GetString(LogLevelKey))
	if err != nil {
		return loggingConfig, err
	}
	logDisplayLevel := v.GetString(LogLevelKey)
	if v.IsSet(LogDisplayLevelKey) {
		logDisplayLevel = v.GetString(LogDisplayLevelKey)
	}
	loggingConfig.DisplayLevel, err = logging.ToLevel(logDisplayLevel)
	if err != nil {
		return loggingConfig, err
	}
	loggingConfig.LogFormat, err = logging.ToFormat(v.GetString(LogFormatKey), os.Stdout.Fd())
	loggingConfig.DisableWriterDisplaying = v.GetBool(LogDisableDisplayPluginLogsKey)
	loggingConfig.MaxSize = int(v.GetUint(LogRotaterMaxSizeKey))
	loggingConfig.MaxFiles = int(v.GetUint(LogRotaterMaxFilesKey))
	loggingConfig.MaxAge = int(v.GetUint(LogRotaterMaxAgeKey))
	loggingConfig.Compress = v.GetBool(LogRotaterCompressEnabledKey)

	return loggingConfig, err
}

func getAPIAuthConfig(v *viper.Viper) (node.APIAuthConfig, error) {
	config := node.APIAuthConfig{
		APIRequireAuthToken: v.GetBool(APIAuthRequiredKey),
	}
	if !config.APIRequireAuthToken {
		return config, nil
	}

	if v.IsSet(APIAuthPasswordKey) {
		config.APIAuthPassword = v.GetString(APIAuthPasswordKey)
	} else {
		passwordFilePath := v.GetString(APIAuthPasswordFileKey) // picks flag value or default
		passwordBytes, err := os.ReadFile(passwordFilePath)
		if err != nil {
			return node.APIAuthConfig{}, fmt.Errorf("API auth password file %q failed to be read: %w", passwordFilePath, err)
		}
		config.APIAuthPassword = strings.TrimSpace(string(passwordBytes))
	}

	if !password.SufficientlyStrong(config.APIAuthPassword, password.OK) {
		return node.APIAuthConfig{}, errAuthPasswordTooWeak
	}
	return config, nil
}

func getIPCConfig(v *viper.Viper) node.IPCConfig {
	config := node.IPCConfig{
		IPCAPIEnabled: v.GetBool(IpcAPIEnabledKey),
		IPCPath:       ipcs.DefaultBaseURL,
	}
	if v.IsSet(IpcsChainIDsKey) {
		config.IPCDefaultChainIDs = strings.Split(v.GetString(IpcsChainIDsKey), ",")
	}
	if v.IsSet(IpcsPathKey) {
		config.IPCPath = GetExpandedArg(v, IpcsPathKey)
	}
	return config
}

func getHTTPConfig(v *viper.Viper) (node.HTTPConfig, error) {
	var (
		httpsKey  []byte
		httpsCert []byte
		err       error
	)
	switch {
	case v.IsSet(HTTPSKeyContentKey):
		rawContent := v.GetString(HTTPSKeyContentKey)
		httpsKey, err = base64.StdEncoding.DecodeString(rawContent)
		if err != nil {
			return node.HTTPConfig{}, fmt.Errorf("unable to decode base64 content: %w", err)
		}
	case v.IsSet(HTTPSKeyFileKey):
		httpsKeyFilepath := GetExpandedArg(v, HTTPSKeyFileKey)
		httpsKey, err = os.ReadFile(filepath.Clean(httpsKeyFilepath))
		if err != nil {
			return node.HTTPConfig{}, err
		}
	}

	switch {
	case v.IsSet(HTTPSCertContentKey):
		rawContent := v.GetString(HTTPSCertContentKey)
		httpsCert, err = base64.StdEncoding.DecodeString(rawContent)
		if err != nil {
			return node.HTTPConfig{}, fmt.Errorf("unable to decode base64 content: %w", err)
		}
	case v.IsSet(HTTPSCertFileKey):
		httpsCertFilepath := GetExpandedArg(v, HTTPSCertFileKey)
		httpsCert, err = os.ReadFile(filepath.Clean(httpsCertFilepath))
		if err != nil {
			return node.HTTPConfig{}, err
		}
	}

	config := node.HTTPConfig{
		APIConfig: node.APIConfig{
			APIIndexerConfig: node.APIIndexerConfig{
				IndexAPIEnabled:      v.GetBool(IndexEnabledKey),
				IndexAllowIncomplete: v.GetBool(IndexAllowIncompleteKey),
			},
			AdminAPIEnabled:    v.GetBool(AdminAPIEnabledKey),
			InfoAPIEnabled:     v.GetBool(InfoAPIEnabledKey),
			KeystoreAPIEnabled: v.GetBool(KeystoreAPIEnabledKey),
			MetricsAPIEnabled:  v.GetBool(MetricsAPIEnabledKey),
			HealthAPIEnabled:   v.GetBool(HealthAPIEnabledKey),
		},
		HTTPHost:          v.GetString(HTTPHostKey),
		HTTPPort:          uint16(v.GetUint(HTTPPortKey)),
		HTTPSEnabled:      v.GetBool(HTTPSEnabledKey),
		HTTPSKey:          httpsKey,
		HTTPSCert:         httpsCert,
		APIAllowedOrigins: v.GetStringSlice(HTTPAllowedOrigins),

		ShutdownTimeout: v.GetDuration(HTTPShutdownTimeoutKey),
		ShutdownWait:    v.GetDuration(HTTPShutdownWaitKey),
	}

	config.APIAuthConfig, err = getAPIAuthConfig(v)
	if err != nil {
		return node.HTTPConfig{}, err
	}
	config.IPCConfig = getIPCConfig(v)
	return config, nil
}

func getRouterHealthConfig(v *viper.Viper, halflife time.Duration) (router.HealthConfig, error) {
	config := router.HealthConfig{
		MaxDropRate:            v.GetFloat64(RouterHealthMaxDropRateKey),
		MaxOutstandingRequests: int(v.GetUint(RouterHealthMaxOutstandingRequestsKey)),
		MaxOutstandingDuration: v.GetDuration(NetworkHealthMaxOutstandingDurationKey),
		MaxRunTimeRequests:     v.GetDuration(NetworkMaximumTimeoutKey),
		MaxDropRateHalflife:    halflife,
	}
	switch {
	case config.MaxDropRate < 0 || config.MaxDropRate > 1:
		return router.HealthConfig{}, fmt.Errorf("%q must be in [0,1]", RouterHealthMaxDropRateKey)
	case config.MaxOutstandingDuration <= 0:
		return router.HealthConfig{}, fmt.Errorf("%q must be positive", NetworkHealthMaxOutstandingDurationKey)
	case config.MaxRunTimeRequests <= 0:
		return router.HealthConfig{}, fmt.Errorf("%q must be positive", NetworkMaximumTimeoutKey)
	}
	return config, nil
}

func getAdaptiveTimeoutConfig(v *viper.Viper) (timer.AdaptiveTimeoutConfig, error) {
	config := timer.AdaptiveTimeoutConfig{
		InitialTimeout:     v.GetDuration(NetworkInitialTimeoutKey),
		MinimumTimeout:     v.GetDuration(NetworkMinimumTimeoutKey),
		MaximumTimeout:     v.GetDuration(NetworkMaximumTimeoutKey),
		TimeoutHalflife:    v.GetDuration(NetworkTimeoutHalflifeKey),
		TimeoutCoefficient: v.GetFloat64(NetworkTimeoutCoefficientKey),
	}
	switch {
	case config.MinimumTimeout < 1:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be positive", NetworkMinimumTimeoutKey)
	case config.MinimumTimeout > config.MaximumTimeout:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be >= %q", NetworkMaximumTimeoutKey, NetworkMinimumTimeoutKey)
	case config.InitialTimeout < config.MinimumTimeout || config.InitialTimeout > config.MaximumTimeout:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be in [%q, %q]", NetworkInitialTimeoutKey, NetworkMinimumTimeoutKey, NetworkMaximumTimeoutKey)
	case config.TimeoutHalflife <= 0:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must > 0", NetworkTimeoutHalflifeKey)
	case config.TimeoutCoefficient < 1:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be >= 1", NetworkTimeoutCoefficientKey)
	}

	return config, nil
}

func getGossipConfig(v *viper.Viper) sender.GossipConfig {
	return sender.GossipConfig{
		AcceptedFrontierValidatorSize:    uint(v.GetUint32(ConsensusGossipAcceptedFrontierValidatorSizeKey)),
		AcceptedFrontierNonValidatorSize: uint(v.GetUint32(ConsensusGossipAcceptedFrontierNonValidatorSizeKey)),
		AcceptedFrontierPeerSize:         uint(v.GetUint32(ConsensusGossipAcceptedFrontierPeerSizeKey)),
		OnAcceptValidatorSize:            uint(v.GetUint32(ConsensusGossipOnAcceptValidatorSizeKey)),
		OnAcceptNonValidatorSize:         uint(v.GetUint32(ConsensusGossipOnAcceptNonValidatorSizeKey)),
		OnAcceptPeerSize:                 uint(v.GetUint32(ConsensusGossipOnAcceptPeerSizeKey)),
		AppGossipValidatorSize:           uint(v.GetUint32(AppGossipValidatorSizeKey)),
		AppGossipNonValidatorSize:        uint(v.GetUint32(AppGossipNonValidatorSizeKey)),
		AppGossipPeerSize:                uint(v.GetUint32(AppGossipPeerSizeKey)),
	}
}

func getNetworkConfig(v *viper.Viper, halflife time.Duration) (network.Config, error) {
	// Set the max number of recent inbound connections upgraded to be
	// equal to the max number of inbound connections per second.
	maxInboundConnsPerSec := v.GetFloat64(InboundThrottlerMaxConnsPerSecKey)
	upgradeCooldown := v.GetDuration(InboundConnUpgradeThrottlerCooldownKey)
	upgradeCooldownInSeconds := upgradeCooldown.Seconds()
	maxRecentConnsUpgraded := int(math.Ceil(maxInboundConnsPerSec * upgradeCooldownInSeconds))
	config := network.Config{
		// Throttling
		ThrottlerConfig: network.ThrottlerConfig{
			MaxInboundConnsPerSec: maxInboundConnsPerSec,
			InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        upgradeCooldown,
				MaxRecentConnsUpgraded: maxRecentConnsUpgraded,
			},

			InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
				MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
					AtLargeAllocSize:    v.GetUint64(InboundThrottlerAtLargeAllocSizeKey),
					VdrAllocSize:        v.GetUint64(InboundThrottlerVdrAllocSizeKey),
					NodeMaxAtLargeBytes: v.GetUint64(InboundThrottlerNodeMaxAtLargeBytesKey),
				},
				BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
					RefillRate:   v.GetUint64(InboundThrottlerBandwidthRefillRateKey),
					MaxBurstSize: v.GetUint64(InboundThrottlerBandwidthMaxBurstSizeKey),
				},
				MaxProcessingMsgsPerNode: v.GetUint64(InboundThrottlerMaxProcessingMsgsPerNodeKey),
				CPUThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: v.GetDuration(InboundThrottlerCPUMaxRecheckDelayKey),
				},
				DiskThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: v.GetDuration(InboundThrottlerDiskMaxRecheckDelayKey),
				},
			},

			OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
				AtLargeAllocSize:    v.GetUint64(OutboundThrottlerAtLargeAllocSizeKey),
				VdrAllocSize:        v.GetUint64(OutboundThrottlerVdrAllocSizeKey),
				NodeMaxAtLargeBytes: v.GetUint64(OutboundThrottlerNodeMaxAtLargeBytesKey),
			},
		},

		HealthConfig: network.HealthConfig{
			MaxTimeSinceMsgSent:          v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey),
			MaxTimeSinceMsgReceived:      v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey),
			MaxPortionSendQueueBytesFull: v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey),
			MinConnectedPeers:            v.GetUint(NetworkHealthMinPeersKey),
			MaxSendFailRate:              v.GetFloat64(NetworkHealthMaxSendFailRateKey),
			SendFailRateHalflife:         halflife,
		},

		DialerConfig: dialer.Config{
			ThrottleRps:       v.GetUint32(OutboundConnectionThrottlingRpsKey),
			ConnectionTimeout: v.GetDuration(OutboundConnectionTimeoutKey),
		},

		TLSKeyLogFile: v.GetString(NetworkTLSKeyLogFileKey),

		TimeoutConfig: network.TimeoutConfig{
			PingPongTimeout:      v.GetDuration(NetworkPingTimeoutKey),
			ReadHandshakeTimeout: v.GetDuration(NetworkReadHandshakeTimeoutKey),
		},

		PeerListGossipConfig: network.PeerListGossipConfig{
			PeerListNumValidatorIPs:        v.GetUint32(NetworkPeerListNumValidatorIPsKey),
			PeerListValidatorGossipSize:    v.GetUint32(NetworkPeerListValidatorGossipSizeKey),
			PeerListNonValidatorGossipSize: v.GetUint32(NetworkPeerListNonValidatorGossipSizeKey),
			PeerListPeersGossipSize:        v.GetUint32(NetworkPeerListPeersGossipSizeKey),
			PeerListGossipFreq:             v.GetDuration(NetworkPeerListGossipFreqKey),
		},

		DelayConfig: network.DelayConfig{
			MaxReconnectDelay:     v.GetDuration(NetworkMaxReconnectDelayKey),
			InitialReconnectDelay: v.GetDuration(NetworkInitialReconnectDelayKey),
		},

		MaxClockDifference:           v.GetDuration(NetworkMaxClockDifferenceKey),
		CompressionEnabled:           v.GetBool(NetworkCompressionEnabledKey),
		PingFrequency:                v.GetDuration(NetworkPingFrequencyKey),
		AllowPrivateIPs:              v.GetBool(NetworkAllowPrivateIPsKey),
		UptimeMetricFreq:             v.GetDuration(UptimeMetricFreqKey),
		MaximumInboundMessageTimeout: v.GetDuration(NetworkMaximumInboundTimeoutKey),

		RequireValidatorToConnect: v.GetBool(NetworkRequireValidatorToConnectKey),
		PeerReadBufferSize:        int(v.GetUint(NetworkPeerReadBufferSizeKey)),
		PeerWriteBufferSize:       int(v.GetUint(NetworkPeerWriteBufferSizeKey)),
	}

	switch {
	case config.HealthConfig.MaxTimeSinceMsgSent < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case config.HealthConfig.MaxTimeSinceMsgReceived < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case config.HealthConfig.MaxSendFailRate < 0 || config.HealthConfig.MaxSendFailRate > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case config.HealthConfig.MaxPortionSendQueueBytesFull < 0 || config.HealthConfig.MaxPortionSendQueueBytesFull > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	case config.DialerConfig.ConnectionTimeout < 0:
		return network.Config{}, fmt.Errorf("%q must be >= 0", OutboundConnectionTimeoutKey)
	case config.PeerListGossipFreq < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkPeerListGossipFreqKey)
	case config.MaxReconnectDelay < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkMaxReconnectDelayKey)
	case config.InitialReconnectDelay < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkInitialReconnectDelayKey)
	case config.MaxReconnectDelay < config.InitialReconnectDelay:
		return network.Config{}, fmt.Errorf("%s must be >= %s", NetworkMaxReconnectDelayKey, NetworkInitialReconnectDelayKey)
	case config.PingPongTimeout < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkPingTimeoutKey)
	case config.PingFrequency < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkPingFrequencyKey)
	case config.PingPongTimeout <= config.PingFrequency:
		return network.Config{}, fmt.Errorf("%s must be > %s", NetworkPingTimeoutKey, NetworkPingFrequencyKey)
	case config.ReadHandshakeTimeout < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkReadHandshakeTimeoutKey)
	case config.MaxClockDifference < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkMaxClockDifferenceKey)
	}
	return config, nil
}

func getBenchlistConfig(v *viper.Viper, alpha, k int) (benchlist.Config, error) {
	config := benchlist.Config{
		Threshold:              v.GetInt(BenchlistFailThresholdKey),
		Duration:               v.GetDuration(BenchlistDurationKey),
		MinimumFailingDuration: v.GetDuration(BenchlistMinFailingDurationKey),
		MaxPortion:             (1.0 - (float64(alpha) / float64(k))) / 3.0,
	}
	switch {
	case config.Duration < 0:
		return benchlist.Config{}, fmt.Errorf("%q must be >= 0", BenchlistDurationKey)
	case config.MinimumFailingDuration < 0:
		return benchlist.Config{}, fmt.Errorf("%q must be >= 0", BenchlistMinFailingDurationKey)
	}
	return config, nil
}

func getStateSyncConfig(v *viper.Viper) (node.StateSyncConfig, error) {
	var (
		config       = node.StateSyncConfig{}
		stateSyncIPs = strings.Split(v.GetString(StateSyncIPsKey), ",")
		stateSyncIDs = strings.Split(v.GetString(StateSyncIDsKey), ",")
	)

	for _, ip := range stateSyncIPs {
		if ip == "" {
			continue
		}
		addr, err := ips.ToIPPort(ip)
		if err != nil {
			return node.StateSyncConfig{}, fmt.Errorf("couldn't parse state sync ip %s: %w", ip, err)
		}
		config.StateSyncIPs = append(config.StateSyncIPs, addr)
	}

	for _, id := range stateSyncIDs {
		if id == "" {
			continue
		}
		nodeID, err := ids.NodeIDFromString(id)
		if err != nil {
			return node.StateSyncConfig{}, fmt.Errorf("couldn't parse state sync peer id %s: %w", id, err)
		}
		config.StateSyncIDs = append(config.StateSyncIDs, nodeID)
	}

	lenIPs := len(config.StateSyncIPs)
	lenIDs := len(config.StateSyncIDs)
	if lenIPs != lenIDs {
		return node.StateSyncConfig{}, fmt.Errorf("expected the number of stateSyncIPs (%d) to match the number of stateSyncIDs (%d)", lenIPs, lenIDs)
	}

	return config, nil
}

func getBootstrapConfig(v *viper.Viper, networkID uint32) (node.BootstrapConfig, error) {
	config := node.BootstrapConfig{
		RetryBootstrap:                          v.GetBool(RetryBootstrapKey),
		RetryBootstrapWarnFrequency:             v.GetInt(RetryBootstrapWarnFrequencyKey),
		BootstrapBeaconConnectionTimeout:        v.GetDuration(BootstrapBeaconConnectionTimeoutKey),
		BootstrapMaxTimeGetAncestors:            v.GetDuration(BootstrapMaxTimeGetAncestorsKey),
		BootstrapAncestorsMaxContainersSent:     int(v.GetUint(BootstrapAncestorsMaxContainersSentKey)),
		BootstrapAncestorsMaxContainersReceived: int(v.GetUint(BootstrapAncestorsMaxContainersReceivedKey)),
	}

	ipsSet := v.IsSet(BootstrapIPsKey)
	idsSet := v.IsSet(BootstrapIDsKey)
	if ipsSet && !idsSet {
		return node.BootstrapConfig{}, fmt.Errorf("set %q but didn't set %q", BootstrapIPsKey, BootstrapIDsKey)
	}
	if !ipsSet && idsSet {
		return node.BootstrapConfig{}, fmt.Errorf("set %q but didn't set %q", BootstrapIDsKey, BootstrapIPsKey)
	}

	bootstrapIPs, bootstrapIDs := genesis.SampleBeacons(networkID, 5)
	if ipsSet {
		bootstrapIPs = strings.Split(v.GetString(BootstrapIPsKey), ",")
	}
	for _, ip := range bootstrapIPs {
		if ip == "" {
			continue
		}
		addr, err := ips.ToIPPort(ip)
		if err != nil {
			return node.BootstrapConfig{}, fmt.Errorf("couldn't parse bootstrap ip %s: %w", ip, err)
		}
		config.BootstrapIPs = append(config.BootstrapIPs, addr)
	}

	if idsSet {
		bootstrapIDs = strings.Split(v.GetString(BootstrapIDsKey), ",")
	}
	for _, id := range bootstrapIDs {
		if id == "" {
			continue
		}
		nodeID, err := ids.NodeIDFromString(id)
		if err != nil {
			return node.BootstrapConfig{}, fmt.Errorf("couldn't parse bootstrap peer id %s: %w", id, err)
		}
		config.BootstrapIDs = append(config.BootstrapIDs, nodeID)
	}

	lenIPs := len(config.BootstrapIPs)
	lenIDs := len(config.BootstrapIDs)
	if lenIPs != lenIDs {
		return node.BootstrapConfig{}, fmt.Errorf("expected the number of bootstrapIPs (%d) to match the number of bootstrapIDs (%d)", lenIPs, lenIDs)
	}

	return config, nil
}

func getIPConfig(v *viper.Viper) (node.IPConfig, error) {
	ipResolutionService := v.GetString(PublicIPResolutionServiceKey)
	ipResolutionFreq := v.GetDuration(PublicIPResolutionFreqKey)
	if ipResolutionFreq <= 0 {
		return node.IPConfig{}, fmt.Errorf("%q must be > 0", PublicIPResolutionFreqKey)
	}

	stakingPort := uint16(v.GetUint(StakingPortKey))
	publicIP := v.GetString(PublicIPKey)
	if publicIP != "" && ipResolutionService != "" {
		return node.IPConfig{}, fmt.Errorf("only one of --%s and --%s can be given", PublicIPKey, PublicIPResolutionServiceKey)
	}

	if publicIP != "" {
		// User specified a specific public IP to use.
		ip := net.ParseIP(publicIP)
		if ip == nil {
			return node.IPConfig{}, fmt.Errorf("invalid IP Address %s", publicIP)
		}
		return node.IPConfig{
			IPPort:           ips.NewDynamicIPPort(ip, stakingPort),
			IPUpdater:        dynamicip.NewNoUpdater(),
			IPResolutionFreq: ipResolutionFreq,
			Nat:              nat.NewNoRouter(),
		}, nil
	}
	if ipResolutionService != "" {
		// User specified to use dynamic IP resolution.
		resolver, err := dynamicip.NewResolver(ipResolutionService)
		if err != nil {
			return node.IPConfig{}, fmt.Errorf("couldn't create IP resolver: %w", err)
		}

		// Use that to resolve our public IP.
		ip, err := resolver.Resolve()
		if err != nil {
			return node.IPConfig{}, fmt.Errorf("couldn't resolve public IP: %w", err)
		}
		ipPort := ips.NewDynamicIPPort(ip, stakingPort)

		return node.IPConfig{
			IPPort: ipPort,
			IPUpdater: dynamicip.NewUpdater(
				ipPort,
				resolver,
				ipResolutionFreq,
			),
			IPResolutionFreq: ipResolutionFreq,
			Nat:              nat.NewNoRouter(),
		}, nil
	}

	// User didn't specify a public IP to use, and they didn't specify a public IP resolution
	// service to use. Try to resolve public IP with NAT traversal.
	nat := nat.GetRouter()
	ip, err := nat.ExternalIP()
	if err != nil {
		return node.IPConfig{}, fmt.Errorf("public IP / IP resolution service not given and failed to resolve IP with NAT: %w", err)
	}
	return node.IPConfig{
		Nat:                   nat,
		AttemptedNATTraversal: true,
		IPPort:                ips.NewDynamicIPPort(ip, stakingPort),
		IPUpdater:             dynamicip.NewNoUpdater(),
		IPResolutionFreq:      ipResolutionFreq,
	}, nil
}

func getProfilerConfig(v *viper.Viper) (profiler.Config, error) {
	config := profiler.Config{
		Dir:         GetExpandedArg(v, ProfileDirKey),
		Enabled:     v.GetBool(ProfileContinuousEnabledKey),
		Freq:        v.GetDuration(ProfileContinuousFreqKey),
		MaxNumFiles: v.GetInt(ProfileContinuousMaxFilesKey),
	}
	if config.Freq < 0 {
		return profiler.Config{}, fmt.Errorf("%s must be >= 0", ProfileContinuousFreqKey)
	}
	return config, nil
}

func getStakingTLSCertFromFlag(v *viper.Viper) (tls.Certificate, error) {
	stakingKeyRawContent := v.GetString(StakingTLSKeyContentKey)
	stakingKeyContent, err := base64.StdEncoding.DecodeString(stakingKeyRawContent)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("unable to decode base64 content: %w", err)
	}

	stakingCertRawContent := v.GetString(StakingCertContentKey)
	stakingCertContent, err := base64.StdEncoding.DecodeString(stakingCertRawContent)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("unable to decode base64 content: %w", err)
	}

	cert, err := staking.LoadTLSCertFromBytes(stakingKeyContent, stakingCertContent)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed creating cert: %w", err)
	}

	return *cert, nil
}

func getStakingTLSCertFromFile(v *viper.Viper) (tls.Certificate, error) {
	// Parse the staking key/cert paths and expand environment variables
	stakingKeyPath := GetExpandedArg(v, StakingTLSKeyPathKey)
	stakingCertPath := GetExpandedArg(v, StakingCertPathKey)

	// If staking key/cert locations are specified but not found, error
	if v.IsSet(StakingTLSKeyPathKey) || v.IsSet(StakingCertPathKey) {
		if _, err := os.Stat(stakingKeyPath); os.IsNotExist(err) {
			return tls.Certificate{}, fmt.Errorf("couldn't find staking key at %s", stakingKeyPath)
		} else if _, err := os.Stat(stakingCertPath); os.IsNotExist(err) {
			return tls.Certificate{}, fmt.Errorf("couldn't find staking certificate at %s", stakingCertPath)
		}
	} else {
		// Create the staking key/cert if [stakingKeyPath] and [stakingCertPath] don't exist
		if err := staking.InitNodeStakingKeyPair(stakingKeyPath, stakingCertPath); err != nil {
			return tls.Certificate{}, fmt.Errorf("couldn't generate staking key/cert: %w", err)
		}
	}

	// Load and parse the staking key/cert
	cert, err := staking.LoadTLSCertFromFiles(stakingKeyPath, stakingCertPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("couldn't read staking certificate: %w", err)
	}
	return *cert, nil
}

func getStakingTLSCert(v *viper.Viper) (tls.Certificate, error) {
	if v.GetBool(StakingEphemeralCertEnabledKey) {
		// Use an ephemeral staking key/cert
		cert, err := staking.NewTLSCert()
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("couldn't generate ephemeral staking key/cert: %w", err)
		}
		return *cert, nil
	}

	switch {
	case v.IsSet(StakingTLSKeyContentKey) && !v.IsSet(StakingCertContentKey):
		return tls.Certificate{}, errStakingCertContentUnset
	case !v.IsSet(StakingTLSKeyContentKey) && v.IsSet(StakingCertContentKey):
		return tls.Certificate{}, errStakingKeyContentUnset
	case v.IsSet(StakingTLSKeyContentKey) && v.IsSet(StakingCertContentKey):
		return getStakingTLSCertFromFlag(v)
	default:
		return getStakingTLSCertFromFile(v)
	}
}

func getStakingSigner(v *viper.Viper) (*bls.SecretKey, error) {
	if v.GetBool(StakingEphemeralSignerEnabledKey) {
		key, err := bls.NewSecretKey()
		if err != nil {
			return nil, fmt.Errorf("couldn't generate ephemeral signing key: %w", err)
		}
		return key, nil
	}

	if v.IsSet(StakingSignerKeyContentKey) {
		signerKeyRawContent := v.GetString(StakingSignerKeyContentKey)
		signerKeyContent, err := base64.StdEncoding.DecodeString(signerKeyRawContent)
		if err != nil {
			return nil, fmt.Errorf("unable to decode base64 content: %w", err)
		}
		key, err := bls.SecretKeyFromBytes(signerKeyContent)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse signing key: %w", err)
		}
		return key, nil
	}

	signingKeyPath := GetExpandedArg(v, StakingSignerKeyPathKey)
	_, err := os.Stat(signingKeyPath)
	if !errors.Is(err, fs.ErrNotExist) {
		signingKeyBytes, err := os.ReadFile(signingKeyPath)
		if err != nil {
			return nil, err
		}
		key, err := bls.SecretKeyFromBytes(signingKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse signing key: %w", err)
		}
		return key, nil
	}

	if v.IsSet(StakingSignerKeyPathKey) {
		return nil, errors.New("missing staking signing key file")
	}

	key, err := bls.NewSecretKey()
	if err != nil {
		return nil, fmt.Errorf("couldn't generate new signing key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(signingKeyPath), perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("couldn't create path for signing key at %s: %w", signingKeyPath, err)
	}

	keyBytes := bls.SecretKeyToBytes(key)
	if err := os.WriteFile(signingKeyPath, keyBytes, perms.ReadWrite); err != nil {
		return nil, fmt.Errorf("couldn't write new signing key to %s: %w", signingKeyPath, err)
	}
	if err := os.Chmod(signingKeyPath, perms.ReadOnly); err != nil {
		return nil, fmt.Errorf("couldn't restrict permissions on new signing key at %s: %w", signingKeyPath, err)
	}
	return key, nil
}

func getStakingConfig(v *viper.Viper, networkID uint32) (node.StakingConfig, error) {
	config := node.StakingConfig{
		EnableStaking:         v.GetBool(StakingEnabledKey),
		DisabledStakingWeight: v.GetUint64(StakingDisabledWeightKey),
		StakingKeyPath:        GetExpandedArg(v, StakingTLSKeyPathKey),
		StakingCertPath:       GetExpandedArg(v, StakingCertPathKey),
		StakingSignerPath:     GetExpandedArg(v, StakingSignerKeyPathKey),
	}
	if !config.EnableStaking && config.DisabledStakingWeight == 0 {
		return node.StakingConfig{}, errInvalidStakerWeights
	}

	if !config.EnableStaking && (networkID == constants.MainnetID || networkID == constants.FujiID) {
		return node.StakingConfig{}, errStakingDisableOnPublicNetwork
	}

	var err error
	config.StakingTLSCert, err = getStakingTLSCert(v)
	if err != nil {
		return node.StakingConfig{}, err
	}
	config.StakingSigningKey, err = getStakingSigner(v)
	if err != nil {
		return node.StakingConfig{}, err
	}
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		config.UptimeRequirement = v.GetFloat64(UptimeRequirementKey)
		config.MinValidatorStake = v.GetUint64(MinValidatorStakeKey)
		config.MaxValidatorStake = v.GetUint64(MaxValidatorStakeKey)
		config.MinDelegatorStake = v.GetUint64(MinDelegatorStakeKey)
		config.MinStakeDuration = v.GetDuration(MinStakeDurationKey)
		config.MaxStakeDuration = v.GetDuration(MaxStakeDurationKey)
		config.RewardConfig.MaxConsumptionRate = v.GetUint64(StakeMaxConsumptionRateKey)
		config.RewardConfig.MinConsumptionRate = v.GetUint64(StakeMinConsumptionRateKey)
		config.RewardConfig.MintingPeriod = v.GetDuration(StakeMintingPeriodKey)
		config.RewardConfig.SupplyCap = v.GetUint64(StakeSupplyCapKey)
		config.MinDelegationFee = v.GetUint32(MinDelegatorFeeKey)
		switch {
		case config.UptimeRequirement < 0 || config.UptimeRequirement > 1:
			return node.StakingConfig{}, errInvalidUptimeRequirement
		case config.MinValidatorStake > config.MaxValidatorStake:
			return node.StakingConfig{}, errMinValidatorStakeAboveMax
		case config.MinDelegationFee > 1_000_000:
			return node.StakingConfig{}, errInvalidDelegationFee
		case config.MinStakeDuration <= 0:
			return node.StakingConfig{}, errInvalidMinStakeDuration
		case config.MaxStakeDuration < config.MinStakeDuration:
			return node.StakingConfig{}, errMinStakeDurationAboveMax
		case config.RewardConfig.MaxConsumptionRate > reward.PercentDenominator:
			return node.StakingConfig{}, errStakeMaxConsumptionTooLarge
		case config.RewardConfig.MaxConsumptionRate < config.RewardConfig.MinConsumptionRate:
			return node.StakingConfig{}, errStakeMaxConsumptionBelowMin
		case config.RewardConfig.MintingPeriod < config.MaxStakeDuration:
			return node.StakingConfig{}, errStakeMintingPeriodBelowMin
		}
	} else {
		config.StakingConfig = genesis.GetStakingConfig(networkID)
	}
	return config, nil
}

func getTxFeeConfig(v *viper.Viper, networkID uint32) genesis.TxFeeConfig {
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		return genesis.TxFeeConfig{
			TxFee:                         v.GetUint64(TxFeeKey),
			CreateAssetTxFee:              v.GetUint64(CreateAssetTxFeeKey),
			CreateSubnetTxFee:             v.GetUint64(CreateSubnetTxFeeKey),
			TransformSubnetTxFee:          v.GetUint64(TransformSubnetTxFeeKey),
			CreateBlockchainTxFee:         v.GetUint64(CreateBlockchainTxFeeKey),
			AddPrimaryNetworkValidatorFee: v.GetUint64(AddPrimaryNetworkValidatorFeeKey),
			AddPrimaryNetworkDelegatorFee: v.GetUint64(AddPrimaryNetworkDelegatorFeeKey),
			AddSubnetValidatorFee:         v.GetUint64(AddSubnetValidatorFeeKey),
			AddSubnetDelegatorFee:         v.GetUint64(AddSubnetDelegatorFeeKey),
		}
	}
	return genesis.GetTxFeeConfig(networkID)
}

func getGenesisData(v *viper.Viper, networkID uint32) ([]byte, ids.ID, error) {
	// try first loading genesis content directly from flag/env-var
	if v.IsSet(GenesisConfigContentKey) {
		genesisData := v.GetString(GenesisConfigContentKey)
		return genesis.FromFlag(networkID, genesisData)
	}

	// if content is not specified go for the file
	if v.IsSet(GenesisConfigFileKey) {
		genesisFileName := GetExpandedArg(v, GenesisConfigFileKey)
		return genesis.FromFile(networkID, genesisFileName)
	}

	// finally if file is not specified/readable go for the predefined config
	config := genesis.GetConfig(networkID)
	return genesis.FromConfig(config)
}

func getWhitelistedSubnets(v *viper.Viper) (set.Set[ids.ID], error) {
	whitelistedSubnetIDs := set.Set[ids.ID]{}
	for _, subnet := range strings.Split(v.GetString(WhitelistedSubnetsKey), ",") {
		if subnet == "" {
			continue
		}
		subnetID, err := ids.FromString(subnet)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse subnetID %q: %w", subnet, err)
		}
		if subnetID == constants.PrimaryNetworkID {
			return nil, errCannotWhitelistPrimaryNetwork
		}
		whitelistedSubnetIDs.Add(subnetID)
	}
	return whitelistedSubnetIDs, nil
}

func getDatabaseConfig(v *viper.Viper, networkID uint32) (node.DatabaseConfig, error) {
	var (
		configBytes []byte
		err         error
	)
	if v.IsSet(DBConfigContentKey) {
		dbConfigContent := v.GetString(DBConfigContentKey)
		configBytes, err = base64.StdEncoding.DecodeString(dbConfigContent)
		if err != nil {
			return node.DatabaseConfig{}, fmt.Errorf("unable to decode base64 content: %w", err)
		}
	} else if v.IsSet(DBConfigFileKey) {
		path := GetExpandedArg(v, DBConfigFileKey)
		configBytes, err = os.ReadFile(path)
		if err != nil {
			return node.DatabaseConfig{}, err
		}
	}

	return node.DatabaseConfig{
		Name: v.GetString(DBTypeKey),
		Path: filepath.Join(
			GetExpandedArg(v, DBPathKey),
			constants.NetworkName(networkID),
		),
		Config: configBytes,
	}, nil
}

func getAliases(v *viper.Viper, name string, contentKey string, fileKey string) (map[ids.ID][]string, error) {
	var fileBytes []byte
	if v.IsSet(contentKey) {
		var err error
		aliasFlagContent := v.GetString(contentKey)
		fileBytes, err = base64.StdEncoding.DecodeString(aliasFlagContent)
		if err != nil {
			return nil, fmt.Errorf("unable to decode base64 content for %s: %w", name, err)
		}
	} else {
		aliasFilePath := filepath.Clean(GetExpandedArg(v, fileKey))
		exists, err := storage.FileExists(aliasFilePath)
		if err != nil {
			return nil, err
		}

		if !exists {
			if v.IsSet(fileKey) {
				return nil, fmt.Errorf("%s file does not exist in %v", name, aliasFilePath)
			}
			return nil, nil
		}

		fileBytes, err = os.ReadFile(aliasFilePath)
		if err != nil {
			return nil, err
		}
	}

	aliasMap := make(map[ids.ID][]string)
	if err := json.Unmarshal(fileBytes, &aliasMap); err != nil {
		return nil, fmt.Errorf("problem unmarshaling %s: %w", name, err)
	}
	return aliasMap, nil
}

func getVMAliases(v *viper.Viper) (map[ids.ID][]string, error) {
	return getAliases(v, "vm aliases", VMAliasesContentKey, VMAliasesFileKey)
}

func getChainAliases(v *viper.Viper) (map[ids.ID][]string, error) {
	return getAliases(v, "chain aliases", ChainAliasesContentKey, ChainAliasesFileKey)
}

func getVMManager(v *viper.Viper) (vms.Manager, error) {
	vmAliases, err := getVMAliases(v)
	if err != nil {
		return nil, err
	}

	manager := vms.NewManager()
	for vmID, aliases := range vmAliases {
		for _, alias := range aliases {
			if err := manager.Alias(vmID, alias); err != nil {
				return nil, err
			}
		}
	}
	return manager, nil
}

// getPathFromDirKey reads flag value from viper instance and then checks the folder existence
func getPathFromDirKey(v *viper.Viper, configKey string) (string, error) {
	configDir := GetExpandedArg(v, configKey)
	cleanPath := filepath.Clean(configDir)
	ok, err := storage.FolderExists(cleanPath)
	if err != nil {
		return "", err
	}
	if ok {
		return cleanPath, nil
	}
	if v.IsSet(configKey) {
		// user specified a config dir explicitly, but dir does not exist.
		return "", fmt.Errorf("cannot read directory: %v", cleanPath)
	}
	return "", nil
}

func getChainConfigsFromFlag(v *viper.Viper) (map[string]chains.ChainConfig, error) {
	chainConfigContentB64 := v.GetString(ChainConfigContentKey)
	chainConfigContent, err := base64.StdEncoding.DecodeString(chainConfigContentB64)
	if err != nil {
		return nil, fmt.Errorf("unable to decode base64 content: %w", err)
	}

	chainConfigs := make(map[string]chains.ChainConfig)
	if err := json.Unmarshal(chainConfigContent, &chainConfigs); err != nil {
		return nil, fmt.Errorf("could not unmarshal JSON: %w", err)
	}
	return chainConfigs, nil
}

func getChainConfigsFromDir(v *viper.Viper) (map[string]chains.ChainConfig, error) {
	chainConfigPath, err := getPathFromDirKey(v, ChainConfigDirKey)
	if err != nil {
		return nil, err
	}

	if len(chainConfigPath) == 0 {
		return make(map[string]chains.ChainConfig), nil
	}

	return readChainConfigPath(chainConfigPath)
}

// getChainConfigs reads & puts chainConfigs to node config
func getChainConfigs(v *viper.Viper) (map[string]chains.ChainConfig, error) {
	if v.IsSet(ChainConfigContentKey) {
		return getChainConfigsFromFlag(v)
	}
	return getChainConfigsFromDir(v)
}

// ReadsChainConfigs reads chain config files from static directories and returns map with contents,
// if successful.
func readChainConfigPath(chainConfigPath string) (map[string]chains.ChainConfig, error) {
	chainDirs, err := filepath.Glob(filepath.Join(chainConfigPath, "*"))
	if err != nil {
		return nil, err
	}
	chainConfigMap := make(map[string]chains.ChainConfig)
	for _, chainDir := range chainDirs {
		dirInfo, err := os.Stat(chainDir)
		if err != nil {
			return nil, err
		}

		if !dirInfo.IsDir() {
			continue
		}

		// chainconfigdir/chainId/config.*
		configData, err := storage.ReadFileWithName(chainDir, chainConfigFileName)
		if err != nil {
			return chainConfigMap, err
		}

		// chainconfigdir/chainId/upgrade.*
		upgradeData, err := storage.ReadFileWithName(chainDir, chainUpgradeFileName)
		if err != nil {
			return chainConfigMap, err
		}

		chainConfigMap[dirInfo.Name()] = chains.ChainConfig{
			Config:  configData,
			Upgrade: upgradeData,
		}
	}
	return chainConfigMap, nil
}

func getSubnetConfigs(v *viper.Viper, subnetIDs []ids.ID) (map[ids.ID]chains.SubnetConfig, error) {
	if v.IsSet(SubnetConfigContentKey) {
		return getSubnetConfigsFromFlags(v, subnetIDs)
	}
	return getSubnetConfigsFromDir(v, subnetIDs)
}

func getSubnetConfigsFromFlags(v *viper.Viper, subnetIDs []ids.ID) (map[ids.ID]chains.SubnetConfig, error) {
	subnetConfigContentB64 := v.GetString(SubnetConfigContentKey)
	subnetConfigContent, err := base64.StdEncoding.DecodeString(subnetConfigContentB64)
	if err != nil {
		return nil, fmt.Errorf("unable to decode base64 content: %w", err)
	}

	// partially parse configs to be filled by defaults later
	subnetConfigs := make(map[ids.ID]json.RawMessage, len(subnetIDs))
	if err := json.Unmarshal(subnetConfigContent, &subnetConfigs); err != nil {
		return nil, fmt.Errorf("could not unmarshal JSON: %w", err)
	}

	res := make(map[ids.ID]chains.SubnetConfig)
	for _, subnetID := range subnetIDs {
		if rawSubnetConfigBytes, ok := subnetConfigs[subnetID]; ok {
			subnetConfig, err := parseSubnetConfigs(rawSubnetConfigBytes, getDefaultSubnetConfig(v))
			if err != nil {
				return nil, err
			}
			res[subnetID] = subnetConfig
		}
	}
	return res, nil
}

// getSubnetConfigs reads SubnetConfigs to node config map
func getSubnetConfigsFromDir(v *viper.Viper, subnetIDs []ids.ID) (map[ids.ID]chains.SubnetConfig, error) {
	subnetConfigPath, err := getPathFromDirKey(v, SubnetConfigDirKey)
	if err != nil {
		return nil, err
	}
	if len(subnetConfigPath) == 0 {
		// subnet config path does not exist but not explicitly specified, so ignore it
		return make(map[ids.ID]chains.SubnetConfig), nil
	}

	// reads subnet config files from a path and given subnetIDs and returns a map.
	subnetConfigs := make(map[ids.ID]chains.SubnetConfig)
	for _, subnetID := range subnetIDs {
		filePath := filepath.Join(subnetConfigPath, subnetID.String()+subnetConfigFileExt)
		fileInfo, err := os.Stat(filePath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// this subnet config does not exist, move to the next one
			continue
		case err != nil:
			return nil, err
		case fileInfo.IsDir():
			return nil, fmt.Errorf("%q is a directory, expected a file", fileInfo.Name())
		}

		// subnetConfigDir/subnetID.json
		file, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		config, err := parseSubnetConfigs(file, getDefaultSubnetConfig(v))
		if err != nil {
			return nil, err
		}
		subnetConfigs[subnetID] = config
	}

	return subnetConfigs, nil
}

func parseSubnetConfigs(data []byte, defaultSubnetConfig chains.SubnetConfig) (chains.SubnetConfig, error) {
	if err := json.Unmarshal(data, &defaultSubnetConfig); err != nil {
		return chains.SubnetConfig{}, err
	}

	if err := defaultSubnetConfig.ConsensusParameters.Valid(); err != nil {
		return chains.SubnetConfig{}, fmt.Errorf("invalid consensus parameters: %w", err)
	}
	return defaultSubnetConfig, nil
}

func getDefaultSubnetConfig(v *viper.Viper) chains.SubnetConfig {
	return chains.SubnetConfig{
		ConsensusParameters:   getConsensusConfig(v),
		ValidatorOnly:         false,
		GossipConfig:          getGossipConfig(v),
		ProposerMinBlockDelay: proposervm.DefaultMinBlockDelay,
	}
}

func getCPUTargeterConfig(v *viper.Viper) (tracker.TargeterConfig, error) {
	vdrAlloc := v.GetFloat64(CPUVdrAllocKey)
	maxNonVdrUsage := v.GetFloat64(CPUMaxNonVdrUsageKey)
	maxNonVdrNodeUsage := v.GetFloat64(CPUMaxNonVdrNodeUsageKey)
	switch {
	case vdrAlloc < 0:
		return tracker.TargeterConfig{}, fmt.Errorf("%q (%f) < 0", CPUVdrAllocKey, vdrAlloc)
	case maxNonVdrUsage < 0:
		return tracker.TargeterConfig{}, fmt.Errorf("%q (%f) < 0", CPUMaxNonVdrUsageKey, maxNonVdrUsage)
	case maxNonVdrNodeUsage < 0:
		return tracker.TargeterConfig{}, fmt.Errorf("%q (%f) < 0", CPUMaxNonVdrNodeUsageKey, maxNonVdrNodeUsage)
	default:
		return tracker.TargeterConfig{
			VdrAlloc:           vdrAlloc,
			MaxNonVdrUsage:     maxNonVdrUsage,
			MaxNonVdrNodeUsage: maxNonVdrNodeUsage,
		}, nil
	}
}

func getDiskSpaceConfig(v *viper.Viper) (requiredAvailableDiskSpace uint64, warningThresholdAvailableDiskSpace uint64, err error) {
	requiredAvailableDiskSpace = v.GetUint64(SystemTrackerRequiredAvailableDiskSpaceKey)
	warningThresholdAvailableDiskSpace = v.GetUint64(SystemTrackerWarningThresholdAvailableDiskSpaceKey)
	switch {
	case warningThresholdAvailableDiskSpace < requiredAvailableDiskSpace:
		return 0, 0, fmt.Errorf("%q (%d) < %q (%d)", SystemTrackerWarningThresholdAvailableDiskSpaceKey, warningThresholdAvailableDiskSpace, SystemTrackerRequiredAvailableDiskSpaceKey, requiredAvailableDiskSpace)
	default:
		return requiredAvailableDiskSpace, warningThresholdAvailableDiskSpace, nil
	}
}

func getDiskTargeterConfig(v *viper.Viper) (tracker.TargeterConfig, error) {
	vdrAlloc := v.GetFloat64(DiskVdrAllocKey)
	maxNonVdrUsage := v.GetFloat64(DiskMaxNonVdrUsageKey)
	maxNonVdrNodeUsage := v.GetFloat64(DiskMaxNonVdrNodeUsageKey)
	switch {
	case vdrAlloc < 0:
		return tracker.TargeterConfig{}, fmt.Errorf("%q (%f) < 0", DiskVdrAllocKey, vdrAlloc)
	case maxNonVdrUsage < 0:
		return tracker.TargeterConfig{}, fmt.Errorf("%q (%f) < 0", DiskMaxNonVdrUsageKey, maxNonVdrUsage)
	case maxNonVdrNodeUsage < 0:
		return tracker.TargeterConfig{}, fmt.Errorf("%q (%f) < 0", DiskMaxNonVdrNodeUsageKey, maxNonVdrNodeUsage)
	default:
		return tracker.TargeterConfig{
			VdrAlloc:           vdrAlloc,
			MaxNonVdrUsage:     maxNonVdrUsage,
			MaxNonVdrNodeUsage: maxNonVdrNodeUsage,
		}, nil
	}
}

func getTraceConfig(v *viper.Viper) (trace.Config, error) {
	enabled := v.GetBool(TracingEnabledKey)
	if !enabled {
		return trace.Config{
			Enabled: false,
		}, nil
	}

	exporterTypeStr := v.GetString(TracingExporterTypeKey)
	exporterType, err := trace.ExporterTypeFromString(exporterTypeStr)
	if err != nil {
		return trace.Config{}, err
	}

	endpoint := v.GetString(TracingEndpointKey)
	if endpoint == "" {
		return trace.Config{}, errTracingEndpointEmpty
	}

	return trace.Config{
		ExporterConfig: trace.ExporterConfig{
			Type:     exporterType,
			Endpoint: endpoint,
			Insecure: v.GetBool(TracingInsecureKey),
			// TODO add support for headers
		},
		Enabled:         true,
		TraceSampleRate: v.GetFloat64(TracingSampleRateKey),
	}, nil
}

func GetNodeConfig(v *viper.Viper, buildDir string) (node.Config, error) {
	nodeConfig := node.Config{}

	// Plugin directory defaults to [buildDir]/[pluginsDirName]
	nodeConfig.PluginDir = filepath.Join(buildDir, pluginsDirName)

	// Consensus Parameters
	nodeConfig.ConsensusParams = getConsensusConfig(v)
	if err := nodeConfig.ConsensusParams.Valid(); err != nil {
		return node.Config{}, err
	}
	nodeConfig.ConsensusShutdownTimeout = v.GetDuration(ConsensusShutdownTimeoutKey)
	if nodeConfig.ConsensusShutdownTimeout < 0 {
		return node.Config{}, fmt.Errorf("%q must be >= 0", ConsensusShutdownTimeoutKey)
	}

	// Gossiping
	nodeConfig.ConsensusGossipFrequency = v.GetDuration(ConsensusGossipFrequencyKey)
	if nodeConfig.ConsensusGossipFrequency < 0 {
		return node.Config{}, fmt.Errorf("%s must be >= 0", ConsensusGossipFrequencyKey)
	}

	nodeConfig.UseCurrentHeight = v.GetBool(ProposerVMUseCurrentHeightKey)

	var err error
	// Logging
	nodeConfig.LoggingConfig, err = getLoggingConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Network ID
	nodeConfig.NetworkID, err = constants.NetworkID(v.GetString(NetworkNameKey))
	if err != nil {
		return node.Config{}, err
	}

	// Database
	nodeConfig.DatabaseConfig, err = getDatabaseConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

	// IP configuration
	nodeConfig.IPConfig, err = getIPConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Staking
	nodeConfig.StakingConfig, err = getStakingConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

	// Whitelisted Subnets
	nodeConfig.WhitelistedSubnets, err = getWhitelistedSubnets(v)
	if err != nil {
		return node.Config{}, err
	}

	// HTTP APIs
	nodeConfig.HTTPConfig, err = getHTTPConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Health
	nodeConfig.HealthCheckFreq = v.GetDuration(HealthCheckFreqKey)
	if nodeConfig.HealthCheckFreq < 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", HealthCheckFreqKey)
	}
	// Halflife of continuous averager used in health checks
	healthCheckAveragerHalflife := v.GetDuration(HealthCheckAveragerHalflifeKey)
	if healthCheckAveragerHalflife <= 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", HealthCheckAveragerHalflifeKey)
	}

	// Router
	nodeConfig.ConsensusRouter = &router.ChainRouter{}
	nodeConfig.RouterHealthConfig, err = getRouterHealthConfig(v, healthCheckAveragerHalflife)
	if err != nil {
		return node.Config{}, err
	}

	// Metrics
	nodeConfig.MeterVMEnabled = v.GetBool(MeterVMsEnabledKey)

	// Adaptive Timeout Config
	nodeConfig.AdaptiveTimeoutConfig, err = getAdaptiveTimeoutConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Network Config
	nodeConfig.NetworkConfig, err = getNetworkConfig(v, healthCheckAveragerHalflife)
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.GossipConfig = getGossipConfig(v)

	// Benchlist
	nodeConfig.BenchlistConfig, err = getBenchlistConfig(v, nodeConfig.ConsensusParams.Alpha, nodeConfig.ConsensusParams.K)
	if err != nil {
		return node.Config{}, err
	}

	// File Descriptor Limit
	nodeConfig.FdLimit = v.GetUint64(FdLimitKey)

	// Tx Fee
	nodeConfig.TxFeeConfig = getTxFeeConfig(v, nodeConfig.NetworkID)

	// Genesis Data
	nodeConfig.GenesisBytes, nodeConfig.AvaxAssetID, err = getGenesisData(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, fmt.Errorf("unable to load genesis file: %w", err)
	}

	// StateSync Configs
	nodeConfig.StateSyncConfig, err = getStateSyncConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Bootstrap Configs
	nodeConfig.BootstrapConfig, err = getBootstrapConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

	// Subnet Configs
	subnetConfigs, err := getSubnetConfigs(v, nodeConfig.WhitelistedSubnets.List())
	if err != nil {
		return node.Config{}, fmt.Errorf("couldn't read subnet configs: %w", err)
	}
	nodeConfig.SubnetConfigs = subnetConfigs

	// Node health
	nodeConfig.MinPercentConnectedStakeHealthy = map[ids.ID]float64{
		constants.PrimaryNetworkID: calcMinConnectedStake(nodeConfig.ConsensusParams.Parameters),
	}

	for subnetID, config := range subnetConfigs {
		nodeConfig.MinPercentConnectedStakeHealthy[subnetID] = calcMinConnectedStake(config.ConsensusParameters.Parameters)
	}

	// Chain Configs
	nodeConfig.ChainConfigs, err = getChainConfigs(v)
	if err != nil {
		return node.Config{}, fmt.Errorf("couldn't read chain configs: %w", err)
	}

	// Profiler
	nodeConfig.ProfilerConfig, err = getProfilerConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// VM Aliases
	nodeConfig.VMManager, err = getVMManager(v)
	if err != nil {
		return node.Config{}, err
	}
	// Chain aliases
	nodeConfig.ChainAliases, err = getChainAliases(v)
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.SystemTrackerFrequency = v.GetDuration(SystemTrackerFrequencyKey)
	nodeConfig.SystemTrackerProcessingHalflife = v.GetDuration(SystemTrackerProcessingHalflifeKey)
	nodeConfig.SystemTrackerCPUHalflife = v.GetDuration(SystemTrackerCPUHalflifeKey)
	nodeConfig.SystemTrackerDiskHalflife = v.GetDuration(SystemTrackerDiskHalflifeKey)

	nodeConfig.RequiredAvailableDiskSpace, nodeConfig.WarningThresholdAvailableDiskSpace, err = getDiskSpaceConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.CPUTargeterConfig, err = getCPUTargeterConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.DiskTargeterConfig, err = getDiskTargeterConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.TraceConfig, err = getTraceConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.ChainDataDir = GetExpandedArg(v, ChainDataDirKey)

	nodeConfig.ProvidedFlags = providedFlags(v)
	return nodeConfig, nil
}

// calcMinConnectedStake takes [consensusParams] as input and calculates the
// expected min connected stake percentage according to alpha and k.
func calcMinConnectedStake(consensusParams snowball.Parameters) float64 {
	alpha := consensusParams.Alpha
	k := consensusParams.K
	r := float64(alpha) / float64(k)
	return r*(1-constants.MinConnectedStakeBuffer) + constants.MinConnectedStakeBuffer
}

func providedFlags(v *viper.Viper) map[string]interface{} {
	settings := v.AllSettings()
	customSettings := make(map[string]interface{}, len(settings))
	for key, val := range settings {
		if v.IsSet(key) {
			customSettings[key] = val
		}
	}
	return customSettings
}
