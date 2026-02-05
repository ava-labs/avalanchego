# MinIO Deployment Implementation Plan

## Status: ✅ COMPLETED

## Overview
Deploy MinIO (open source S3) to a local Kubernetes KIND cluster to enable future integration testing of features needing to transfer data to and from S3.

**Implementation Status**: All tasks have been successfully completed. The MinIO deployment feature is fully functional and tested.

## Implementation Tasks (All Completed ✅)

### 1. ✅ Add MinIO Installation Flag to tmpnetctl
- **File**: `tests/fixture/tmpnet/tmpnetctl/main.go`
  - Add `--install-minio` boolean flag to the `start-kind-cluster` command, similar to `--install-chaos-mesh`
  - Pass this flag to the `StartKindCluster` function

**Implementation Details:**
- The flag is added as a boolean variable `installMinio` in the same scope as `installChaosMesh`
- Uses cobra's `BoolVar` method to bind the flag to the variable
- The flag description clearly indicates it installs "S3-compatible storage"
- The flag is passed as the last parameter to `StartKindCluster`, maintaining the order of parameters

### 2. ✅ Update StartKindCluster Function
- **File**: `tests/fixture/tmpnet/start_kind_cluster.go`
  - Add `installMinio bool` parameter to `StartKindCluster` function
  - Add MinIO constants (namespace, release name, chart repo, etc.) similar to Chaos Mesh constants:
    ```go
    minioNamespace      = "minio"
    minioReleaseName    = "minio"
    minioChartRepo      = "https://charts.min.io"
    minioChartName      = "minio/minio"
    minioChartVersion   = "5.4.0"
    minioDeploymentName = "minio"
    minioConsoleHost    = "minio.localhost"
    minioAPIHost        = "minio-api.localhost"
    ```
  - Create `deployMinio` function that:
    - Checks if MinIO is already running
    - Adds MinIO Helm repository
    - Installs MinIO with appropriate values
    - Configures ingress for MinIO console and API
    - Sets up persistent volume
    - Waits for deployment readiness
  - Add MinIO deployment call in the main function flow after Chaos Mesh

**Implementation Details:**
- Constants are kept unexported (lowercase) following the pattern of other deployment constants
- The `deployMinio` function follows the exact pattern of `deployChaosMesh`
- Uses `isMinioRunning` helper function that checks for deployment existence
- Deployment happens after Chaos Mesh to maintain deployment order
- Uses `waitForDeployment` utility function for consistency
- Logs successful installation with access details

### 3. ✅ Configure MinIO with Ingress
- **MinIO Helm Values**:
  - Set via `--set` flags in Helm install command
  - Standalone mode for simplicity
  - 10Gi persistent volume with standard storage class
  - Both API and Console ingress enabled
  - Uses nginx ingress class
  - Resource limits set to 512Mi memory
  - Default credentials: minioadmin/minioadmin

**Implementation Details:**
- Helm values are passed as command-line arguments rather than a values file
- Uses string concatenation for host values (e.g., `"--set", "ingress.hosts[0]=" + minioAPIHost`)
- The `--wait` flag ensures deployment completes before continuing
- Creates namespace automatically with `--create-namespace`
- Version is pinned to ensure reproducibility

### 4. ✅ Update Dependencies
- Added AWS SDK Go v2 to `go.mod`:
  ```
  github.com/aws/aws-sdk-go-v2
  github.com/aws/aws-sdk-go-v2/config
  github.com/aws/aws-sdk-go-v2/service/s3
  github.com/aws/aws-sdk-go-v2/credentials
  ```

**Implementation Details:**
- SDK v2 chosen over v1 for better performance and modern API
- Only S3 service imported (not entire SDK)
- Static credentials provider used for simplicity
- Dependencies managed with `go mod tidy`

## Implementation Lessons Learned

### Import Organization
- Imports must follow specific ordering for `gci` linter:
  1. Standard library
  2. Third-party packages
  3. Project packages
  4. Kubernetes packages (treated specially)
- K8s imports go in a separate block after project imports


### Linting Compliance
- Use `_` for unused parameters in function literals
- Prefer `errors.New` over `fmt.Errorf` for static error messages
- Use string concatenation over `fmt.Sprintf` for simple cases
- Always run `scripts/run_task.sh lint-all` to catch issues early

## Testing Strategy
- Manual testing: `tmpnetctl start-kind-cluster --install-minio`
- Verify MinIO console accessible at `http://minio.localhost:30791`
- Run integration tests: `go test -v ./tests/fixture/tmpnet -run TestMinioIntegration`
- Test passes all S3 operations successfully

## Success Criteria
✓ MinIO successfully deploys to KIND cluster when flag is provided
✓ MinIO console and S3 API are accessible via ingress
✓ No impact on existing functionality when flag is not provided
✓ Code passes all linting checks
✓ Test file can be removed cleanly before PR
