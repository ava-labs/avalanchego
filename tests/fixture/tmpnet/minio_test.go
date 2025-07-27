// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test-specific MinIO credentials
const (
	minioAccessKey = "minioadmin"
	minioSecretKey = "minioadmin"
)

// getMinioTestS3Client creates an S3 client configured for MinIO testing
func getMinioTestS3Client(ctx context.Context) (*s3.Client, error) {
	// Use constants from start_kind_cluster.go
	minioEndpoint := fmt.Sprintf("%s:%d", minioAPIHost, ingressNodePort)

	// Create custom AWS config for MinIO
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, _ string, _ ...interface{}) (aws.Endpoint, error) {
		if service == s3.ServiceID {
			return aws.Endpoint{
				URL:               "http://" + minioEndpoint,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, errors.New("unknown endpoint requested")
	})

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(minioAccessKey, minioSecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create S3 client with path-style addressing for MinIO
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return client, nil
}

// isMinioInstalled checks if MinIO is installed in the cluster
func isMinioInstalled(ctx context.Context, log logging.Logger, kubeconfig, kubeconfigContext string) (bool, error) {
	clientset, err := GetClientset(log, kubeconfig, kubeconfigContext)
	if err != nil {
		return false, fmt.Errorf("failed to get clientset: %w", err)
	}

	// Check if MinIO namespace exists
	_, err = clientset.CoreV1().Namespaces().Get(ctx, minioNamespace, metav1.GetOptions{})
	if err != nil {
		log.Debug("MinIO namespace not found",
			zap.String("namespace", minioNamespace),
			zap.Error(err),
		)
		return false, nil
	}

	// Check if MinIO deployment exists and is ready
	deployment, err := clientset.AppsV1().Deployments(minioNamespace).Get(ctx, minioDeploymentName, metav1.GetOptions{})
	if err != nil {
		log.Debug("MinIO deployment not found",
			zap.String("namespace", minioNamespace),
			zap.String("deployment", minioDeploymentName),
			zap.Error(err),
		)
		return false, nil
	}

	isReady := deployment.Status.ReadyReplicas > 0
	log.Debug("MinIO deployment status",
		zap.String("namespace", minioNamespace),
		zap.String("deployment", minioDeploymentName),
		zap.Int32("readyReplicas", deployment.Status.ReadyReplicas),
		zap.Bool("isReady", isReady),
	)

	return isReady, nil
}

// TestMinioIntegration verifies MinIO deployment and S3 operations
func TestMinioIntegration(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Use environment variables to get kubeconfig path and context
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.ExpandEnv("$HOME/.kube/config")
	}
	kubeconfigContext := os.Getenv("KUBECONFIG_CONTEXT")
	if kubeconfigContext == "" {
		kubeconfigContext = KindKubeconfigContext
	}

	ctx := context.Background()
	log := logging.NoLog{}

	// Check if MinIO is installed
	installed, err := isMinioInstalled(ctx, log, kubeconfig, kubeconfigContext)
	require.NoError(t, err)
	if !installed {
		t.Skip("MinIO is not installed in the cluster. Run with --install-minio flag")
	}

	// Get MinIO S3 client with hard-coded configuration
	client, err := getMinioTestS3Client(ctx)
	require.NoError(t, err)

	// Test bucket name
	bucketName := "tmpnet-test-bucket"

	// Test 1: Create bucket
	t.Run("CreateBucket", func(t *testing.T) {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)
	})

	// Test 2: List buckets
	t.Run("ListBuckets", func(t *testing.T) {
		result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		require.NoError(t, err)
		require.NotEmpty(t, result.Buckets)

		// Verify our bucket exists
		found := false
		for _, bucket := range result.Buckets {
			if *bucket.Name == bucketName {
				found = true
				break
			}
		}
		require.True(t, found, "Created bucket not found in list")
	})

	// Test 3: Upload object
	testKey := "test-file.txt"
	testContent := []byte("Hello from tmpnet MinIO test!")

	t.Run("UploadObject", func(t *testing.T) {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(testKey),
			Body:   bytes.NewReader(testContent),
		})
		require.NoError(t, err)
	})

	// Test 4: Download object
	t.Run("DownloadObject", func(t *testing.T) {
		result, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(testKey),
		})
		require.NoError(t, err)
		defer result.Body.Close()

		// Read and verify content
		downloadedContent, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Equal(t, testContent, downloadedContent)
	})

	// Test 5: List objects
	t.Run("ListObjects", func(t *testing.T) {
		result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)
		require.Len(t, result.Contents, 1)
		require.Equal(t, testKey, *result.Contents[0].Key)
	})

	// Test 6: Delete object
	t.Run("DeleteObject", func(t *testing.T) {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(testKey),
		})
		require.NoError(t, err)

		// Verify object is deleted
		result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)
		require.Empty(t, result.Contents)
	})

	// Test 7: Delete bucket
	t.Run("DeleteBucket", func(t *testing.T) {
		_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		})
		require.NoError(t, err)
	})

	// Log MinIO access information for reference
	t.Logf("MinIO Console URL: http://%s:%d", minioConsoleHost, ingressNodePort)
	t.Logf("MinIO S3 Endpoint: http://%s:%d", minioAPIHost, ingressNodePort)
	t.Logf("MinIO Credentials: %s/%s", minioAccessKey, minioSecretKey)
}
