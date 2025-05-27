package grpcapi

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func NewService(
	networkID uint32,
	localPrefix ids.ID,
	testnetPrefix ids.ID,
	mainnetPrefix ids.ID,
	desc grpc.ServiceDesc,
) (grpc.ServiceDesc, error) {
	p, err := prefix(networkID, localPrefix, testnetPrefix, mainnetPrefix)
	if err != nil {
		return grpc.ServiceDesc{}, fmt.Errorf("failed to get service prefix: %w", err)
	}

	sd := desc
	sd.ServiceName = fmt.Sprintf("%s.%s", p, sd.ServiceName)

	return sd, nil
}

func NewClient(
	uri string,
	networkID uint32,
	localPrefix ids.ID,
	testnetPrefix ids.ID,
	mainnetPrefix ids.ID,
) (*grpc.ClientConn, error) {
	s, err := prefix(networkID, localPrefix, testnetPrefix, mainnetPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to get client prefix: %w", err)
	}

	return grpc.NewClient(
		uri,
		grpc.WithUnaryInterceptor(prefixServiceNameInterceptor(s)),
	)
}

func prefixServiceNameInterceptor(prefix string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req interface{},
		reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		parts := strings.Split(method, "/") // /Service/Method
		if len(parts) == 3 {
			method = fmt.Sprintf("/%s.%s/%s", prefix, parts[1], parts[2])
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// func RegisterService(sd *ServiceDesc, ss any) {
//}
//
// type Service struct{}

// NewService prefixes a grpc service to disambiguate across multiple instances
// of the same VM
func prefix(
	networkID uint32,
	localPrefix ids.ID,
	testnetPrefix ids.ID,
	mainnetPrefix ids.ID,
) (string, error) {
	switch networkID {
	case constants.LocalID:
		return localPrefix.String(), nil
	case constants.TestnetID:
		return testnetPrefix.String(), nil
	case constants.MainnetID:
		return mainnetPrefix.String(), nil
	default:
		return "", fmt.Errorf("unknown network id %d", networkID)
	}
}
