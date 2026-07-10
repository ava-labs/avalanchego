package config

// AWSConfig holds the KMS endpoint configuration. Credentials are resolved by
// the AWS SDK default credential chain, never passed in here.
type AWSConfig struct {
	// Host, when set, overrides the KMS base endpoint (e.g. a local KMS).
	Host string
	// Region, when set, overrides the region the SDK would otherwise resolve
	// from AWS_REGION / AWS_DEFAULT_REGION.
	Region string
}
