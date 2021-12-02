// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// getIPs returns the beacon IPs for each network
func getIPs(networkID uint32) []string {
	switch networkID {
	case constants.MainnetID:
		return []string{
			"54.94.43.49:9651",
			"52.79.47.77:9651",
			"18.229.206.191:9651",
			"3.34.221.73:9651",
			"13.244.155.170:9651",
			"13.244.47.224:9651",
			"122.248.200.212:9651",
			"52.30.9.211:9651",
			"122.248.199.127:9651",
			"18.202.190.40:9651",
			"15.206.182.45:9651",
			"15.207.11.193:9651",
			"44.226.118.72:9651",
			"54.185.87.50:9651",
			"18.158.15.12:9651",
			"3.21.38.33:9651",
			"54.93.182.129:9651",
			"3.128.138.36:9651",
			"3.104.107.241:9651",
			"3.106.25.139:9651",
			"18.162.129.129:9651",
			"18.162.161.230:9651",
			"52.47.181.114:9651",
			"15.188.9.42:9651",
		}
	case constants.FujiID:
		return []string{
			"3.214.61.227:9651",
			"52.206.218.4:9651",
			"44.194.128.146:9651",
			"3.143.146.90:9651",
			"3.142.66.84:9651",
			"3.142.32.15:9651",
			"44.240.251.247:9651",
			"44.224.22.217:9651",
			"52.13.58.52:9651",
			"18.163.142.196:9651",
			"16.162.54.143:9651",
			"18.167.153.71:9651",
			"52.29.183.160:9651",
			"18.159.63.226:9651",
			"3.65.152.247:9651",
			"34.247.100.96:9651",
			"34.250.89.215:9651",
			"54.228.143.65:9651",
			"54.232.253.20:9651",
			"54.94.159.80:9651",
			"54.94.242.98:9651",
		}
	default:
		return nil
	}
}

// getNodeIDs returns the beacon node IDs for each network
func getNodeIDs(networkID uint32) []string {
	switch networkID {
	case constants.MainnetID:
		return []string{
			"NodeID-A6onFGyJjA37EZ7kYHANMR1PFRT8NmXrF",
			"NodeID-6SwnPJLH8cWfrJ162JjZekbmzaFpjPcf",
			"NodeID-GSgaA47umS1px2ohVjodW9621Ks63xDxD",
			"NodeID-BQEo5Fy1FRKLbX51ejqDd14cuSXJKArH2",
			"NodeID-Drv1Qh7iJvW3zGBBeRnYfCzk56VCRM2GQ",
			"NodeID-DAtCoXfLT6Y83dgJ7FmQg8eR53hz37J79",
			"NodeID-FGRoKnyYKFWYFMb6Xbocf4hKuyCBENgWM",
			"NodeID-Dw7tuwxpAmcpvVGp9JzaHAR3REPoJ8f2R",
			"NodeID-4kCLS16Wy73nt1Zm54jFZsL7Msrv3UCeJ",
			"NodeID-9T7NXBFpp8LWCyc58YdKNoowDipdVKAWz",
			"NodeID-6ghBh6yof5ouMCya2n9fHzhpWouiZFVVj",
			"NodeID-HiFv1DpKXkAAfJ1NHWVqQoojjznibZXHP",
			"NodeID-Fv3t2shrpkmvLnvNzcv1rqRKbDAYFnUor",
			"NodeID-AaxT2P4uuPAHb7vAD8mNvjQ3jgyaV7tu9",
			"NodeID-kZNuQMHhydefgnwjYX1fhHMpRNAs9my1",
			"NodeID-A7GwTSd47AcDVqpTVj7YtxtjHREM33EJw",
			"NodeID-Hr78Fy8uDYiRYocRYHXp4eLCYeb8x5UuM",
			"NodeID-9CkG9MBNavnw7EVSRsuFr7ws9gascDQy3",
			"NodeID-A8jypu63CWp76STwKdqP6e9hjL675kdiG",
			"NodeID-HsBEx3L71EHWSXaE6gvk2VsNntFEZsxqc",
			"NodeID-Nr584bLpGgbCUbZFSBaBz3Xum5wpca9Ym",
			"NodeID-QKGoUvqcgormCoMj6yPw9isY7DX9H4mdd",
			"NodeID-HCw7S2TVbFPDWNBo1GnFWqJ47f9rDJtt1",
			"NodeID-FYv1Lb29SqMpywYXH7yNkcFAzRF2jvm3K",
		}
	case constants.FujiID:
		return []string{
			"NodeID-2m38qc95mhHXtrhjyGbe7r2NhniqHHJRB",
			"NodeID-JjvzhxnLHLUQ5HjVRkvG827ivbLXPwA9u",
			"NodeID-LegbVf6qaMKcsXPnLStkdc1JVktmmiDxy",
			"NodeID-HGZ8ae74J3odT8ESreAdCtdnvWG1J4X5n",
			"NodeID-CYKruAjwH1BmV3m37sXNuprbr7dGQuJwG",
			"NodeID-4KXitMCoE9p2BHA6VzXtaTxLoEjNDo2Pt",
			"NodeID-LQwRLm4cbJ7T2kxcxp4uXCU5XD8DFrE1C",
			"NodeID-4CWTbdvgXHY1CLXqQNAp22nJDo5nAmts6",
			"NodeID-4QBwET5o8kUhvt9xArhir4d3R25CtmZho",
			"NodeID-JyE4P8f4cTryNV8DCz2M81bMtGhFFHexG",
			"NodeID-EDESh4DfZFC15i613pMtWniQ9arbBZRnL",
			"NodeID-BFa1padLXBj7VHa2JYvYGzcTBPQGjPhUy",
			"NodeID-CZmZ9xpCzkWqjAyS7L4htzh5Lg6kf1k18",
			"NodeID-FesGqwKq7z5nPFHa5iwZctHE5EZV9Lpdq",
			"NodeID-84KbQHSDnojroCVY7vQ7u9Tx7pUonPaS",
			"NodeID-CTtkcXvVdhpNp6f97LEUXPwsRD3A2ZHqP",
			"NodeID-hArafGhY2HFTbwaaVh1CSCUCUCiJ2Vfb",
			"NodeID-4B4rc5vdD1758JSBYL1xyvE5NHGzz6xzH",
			"NodeID-EzGaipqomyK9UKx9DBHV6Ky3y68hoknrF",
			"NodeID-NpagUxt6KQiwPch9Sd4osv8kD1TZnkjdk",
			"NodeID-3VWnZNViBP2b56QBY7pNJSLzN2rkTyqnK",
		}
	default:
		return nil
	}
}

// SampleBeacons returns the some beacons this node should connect to
func SampleBeacons(networkID uint32, count int) ([]string, []string) {
	ips := getIPs(networkID)
	ids := getNodeIDs(networkID)

	if numIPs := len(ips); numIPs < count {
		count = numIPs
	}

	sampledIPs := make([]string, 0, count)
	sampledIDs := make([]string, 0, count)

	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(ips)))
	indices, _ := s.Sample(count)
	for _, index := range indices {
		sampledIPs = append(sampledIPs, ips[int(index)])
		sampledIDs = append(sampledIDs, ids[int(index)])
	}

	return sampledIPs, sampledIDs
}
