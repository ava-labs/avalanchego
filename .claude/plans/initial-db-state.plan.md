# Implementation Plan: Deploying Networks with Initial Database State

## Overview
This plan details the implementation of support for deploying tmpnet networks with pre-populated database state. The feature will be limited to Kubernetes runtime deployments due to expected large database sizes (~150GiB).

## Architecture Analysis

### Current State
- **Runtime Abstraction**: Two implementations exist - `ProcessRuntime` (local) and `KubeRuntime` (Kubernetes)
- **StatefulSet Creation**: Uses `NewNodeStatefulSet()` function in `kube.go`
- **Init Containers**: Pattern already exists (see prometheus-agent.yaml example)
- **Flag Handling**: Structured through `flags/` package with runtime-specific configurations

### Key Integration Points
1. **Flag Registration**: `flags/kube_runtime.go` handles Kubernetes-specific flags
2. **Configuration**: `KubeRuntimeConfig` struct stores runtime configuration
3. **StatefulSet Creation**: `kube.go:NewNodeStatefulSet()` creates node deployments
4. **Runtime Start**: `kube_runtime.go:Start()` orchestrates node startup

## Implementation Steps

### 1. Add Configuration Support (flags/kube_runtime.go)

Add new flag to the `kubeRuntimeVars` struct:
```go
type kubeRuntimeVars struct {
    // ... existing fields ...
    initialDBArchive string  // New field
}
```

Register the flag in the `register` method:
```go
stringVar(
    &v.initialDBArchive,
    "kube-initial-db-archive",
    "",
    kubeDocPrefix+"S3 URL to the initial database archive (tar.gz format) to download before starting nodes",
)
```

Update `getKubeRuntimeConfig()` to include the new field:
```go
return &tmpnet.KubeRuntimeConfig{
    // ... existing fields ...
    InitialDBArchive: v.initialDBArchive,
}, nil
```

### 2. Extend KubeRuntimeConfig (kube_runtime.go)

Add field to the `KubeRuntimeConfig` struct:
```go
type KubeRuntimeConfig struct {
    // ... existing fields ...
    // URL or path to initial database archive
    InitialDBArchive string `json:"initialDBArchive,omitempty"`
}

// GetInitialDBPVCName returns the PVC name for the initial database archive
func (c *KubeRuntimeConfig) GetInitialDBPVCName() string {
    if c.InitialDBArchive == "" {
        return ""
    }
    urlParts := strings.Split(c.InitialDBArchive, "/")
    filename := urlParts[len(urlParts)-1]
    filebase := strings.TrimSuffix(filename, ".tar.gz")
    return fmt.Sprintf("initial-db-%s", filebase)
}
```

### 3. Create Shared PVC for Database Archive

Add a new method in `kube_runtime.go` to create a shared PVC:
```go
func (p *KubeRuntime) createSharedDBPVC(ctx context.Context, archiveURL string) (string, error) {
    pvcName := p.runtimeConfig().GetInitialDBPVCName()

    // Check if PVC already exists
    _, err := p.client.CoreV1().PersistentVolumeClaims(p.runtimeConfig().Namespace).Get(ctx, pvcName, metav1.GetOptions{})
    if err == nil {
        // PVC already exists, reuse it
        p.logger.Info("Reusing existing initial DB PVC", zap.String("pvc", pvcName))
        return pvcName, nil
    } else if !apierrors.IsNotFound(err) {
        return "", fmt.Errorf("failed to check PVC existence: %w", err)
    }

    pvc := &corev1.PersistentVolumeClaim{
        ObjectMeta: metav1.ObjectMeta{
            Name:      pvcName,
            Namespace: p.runtimeConfig().Namespace,
        },
        Spec: corev1.PersistentVolumeClaimSpec{
            AccessModes: []corev1.PersistentVolumeAccessMode{
                corev1.ReadWriteOnce, // Allow job to write, then nodes to read
            },
            Resources: corev1.VolumeResourceRequirements{
                Requests: corev1.ResourceList{
                    corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", p.runtimeConfig().VolumeSizeGB)),
                },
            },
        },
    }

    // Create PVC...
    return pvcName, nil
}
```

### 4. Create Download Job

Add a method to create a Kubernetes Job that downloads and extracts the archive:
```go
func (p *KubeRuntime) createDBDownloadJob(ctx context.Context, pvcName string, archiveURL string) error {
    jobName := fmt.Sprintf("%s-db-download", p.node.network.UUID)

    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      jobName,
            Namespace: p.runtimeConfig().Namespace,
        },
        Spec: batchv1.JobSpec{
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy: corev1.RestartPolicyOnFailure,
                    Containers: []corev1.Container{
                        {
                            Name:  "download-db",
                            Image: "alpine:latest",
                            Command: []string{"/bin/sh", "-c"},
                            Args: []string{
                                fmt.Sprintf(`
                                    set -e
                                    echo "Downloading database archive from %s..."
                                    wget -O /tmp/db.tar.gz "%s"
                                    echo "Extracting database archive..."
                                    tar -xzf /tmp/db.tar.gz -C /shared-db
                                    echo "Database download and extraction complete"
                                `, archiveURL, archiveURL),
                            },
                            VolumeMounts: []corev1.VolumeMount{
                                {
                                    Name:      "shared-db",
                                    MountPath: "/shared-db",
                                },
                            },
                        },
                    },
                    Volumes: []corev1.Volume{
                        {
                            Name: "shared-db",
                            VolumeSource: corev1.VolumeSource{
                                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                                    ClaimName: pvcName,
                                },
                            },
                        },
                    },
                },
            },
        },
    }

    // Create job and wait for completion...
    return nil
}
```

### 5. Modify StatefulSet Creation (kube.go)

Update `NewNodeStatefulSet()` to accept optional parameters for initial DB:
```go
func NewNodeStatefulSet(
    // ... existing parameters ...
    sharedDBPVCName string, // New parameter
) *appsv1.StatefulSet {
    // ... existing code ...

    if sharedDBPVCName != "" {
        // Add shared DB volume
        statefulSet.Spec.Template.Spec.Volumes = append(
            statefulSet.Spec.Template.Spec.Volumes,
            corev1.Volume{
                Name: "shared-db",
                VolumeSource: corev1.VolumeSource{
                    PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                        ClaimName: sharedDBPVCName,
                        ReadOnly:  true,
                    },
                },
            },
        )

        // Add init container to copy DB
        statefulSet.Spec.Template.Spec.InitContainers = []corev1.Container{
            {
                Name:  "init-db",
                Image: "alpine:latest",
                Command: []string{"/bin/sh", "-c"},
                Args: []string{`
                    if [ ! -f /data/.db-initialized ]; then
                        echo "Copying initial database state..."
                        cp -r /shared-db/* /data/
                        touch /data/.db-initialized
                        echo "Database initialization complete"
                    else
                        echo "Database already initialized, skipping..."
                    fi
                `},
                VolumeMounts: []corev1.VolumeMount{
                    {
                        Name:      "shared-db",
                        MountPath: "/shared-db",
                        ReadOnly:  true,
                    },
                    {
                        Name:      volumeName,
                        MountPath: volumeMountPath,
                    },
                },
            },
        }
    }

    return statefulSet
}
```

### 6. Update Bootstrap Method (network.go)

Modify the `Bootstrap()` method in network.go to handle initial DB setup before subnet creation:
```go
func (n *Network) Bootstrap(ctx context.Context) error {
    // Check if initial DB is configured for Kubernetes runtime
    if runtime, ok := n.runtimeConfig.(*KubeRuntimeConfig); ok && runtime.InitialDBArchive != "" {
        // Skip subnet creation if using initial DB
        n.logger.Info("Skipping subnet creation due to initial DB configuration")
        return nil
    }

    // ... existing subnet creation code ...
}
```

### 7. Update Start Method (kube_runtime.go)

Modify the `Start()` method to handle initial DB setup:
```go
func (p *KubeRuntime) Start(ctx context.Context) error {
    // ... existing code ...

    // Check if this is the first node and if initial DB is configured
    var sharedDBPVCName string
    if p.node.network.isFirstNode(p.node) && len(runtimeConfig.InitialDBArchive) > 0 {
        // Create shared PVC
        pvcName, err := p.createSharedDBPVC(ctx, runtimeConfig.InitialDBArchive)
        if err != nil {
            return fmt.Errorf("failed to create shared DB PVC: %w", err)
        }
        sharedDBPVCName = pvcName

        // Create and wait for download job
        if err := p.createDBDownloadJob(ctx, pvcName, runtimeConfig.InitialDBArchive); err != nil {
            return fmt.Errorf("failed to download initial DB: %w", err)
        }
    } else if len(runtimeConfig.InitialDBArchive) > 0 {
        // For subsequent nodes, use the existing PVC
        sharedDBPVCName = runtimeConfig.GetInitialDBPVCName()
    }

    // Pass the shared PVC name to StatefulSet creation
    statefulSet := NewNodeStatefulSet(
        // ... existing parameters ...
        sharedDBPVCName, // New parameter
    )

    // ... rest of the method ...
}
```

### 8. Cleanup Logic

The shared PVC will NOT be automatically deleted when the network is destroyed, as it can be reused across multiple test networks. This avoids the need to re-download and extract large database archives.

Users can manually delete the PVC if needed:
```bash
kubectl delete pvc initial-db-<filebase> -n <namespace>
```

### 9. Add tmpnetctl Commands for Local Network Support

#### 9a. init-local-network Command

Add a command to initialize a local network and export its database:

```go
// In cmd/tmpnetctl/cmd/initLocalNetwork.go
func newInitLocalNetworkCmd() *cobra.Command {
    var bootstrapDBPath string

    cmd := &cobra.Command{
        Use:   "init-local-network",
        Short: "Initialize a local network and export its database",
        RunE: func(cmd *cobra.Command, args []string) error {
            if bootstrapDBPath == "" {
                return fmt.Errorf("--bootstrap-db-path is required")
            }

            // Use LocalNetworkOrPanic to ensure consistent genesis
            network := tmpnet.LocalNetworkOrPanic()

            // Bootstrap network and export DB
            ctx := context.Background()
            if err := initBootstrapDB(network, bootstrapDBPath); err != nil {
                return fmt.Errorf("failed to create bootstrap DB: %w", err)
            }
            fmt.Printf("Bootstrap DB created at: %s\n", bootstrapDBPath)
            return nil
        },
    }

    cmd.Flags().StringVar(&bootstrapDBPath, "bootstrap-db-path", "",
        "Path to export the bootstrapped database (required)")
    cmd.MarkFlagRequired("bootstrap-db-path")

    return cmd
}
```

#### 9b. start-local-network Command

Add a command to start a local network using LocalNetworkOrPanic:

```go
// In cmd/tmpnetctl/cmd/startLocalNetwork.go
func newStartLocalNetworkCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "start-local-network",
        Short: "Start a local network using consistent genesis configuration",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Use LocalNetworkOrPanic to ensure consistent genesis
            network := tmpnet.LocalNetworkOrPanic()

            // Start the network
            ctx := context.Background()
            if err := tmpnet.BootstrapNewNetwork(ctx, logger, network, ""); err != nil {
                return fmt.Errorf("failed to bootstrap network: %w", err)
            }

            // Keep the network running
            fmt.Println("Network started successfully. Press Ctrl+C to stop.")
            // Wait for interrupt signal
            <-make(chan struct{})
            return nil
        },
    }

    return cmd
}
```

## Usage Example

```bash
./bin/tmpnetctl start-network \
  --runtime=kube \
  --kube-initial-db-archive=s3://my-bucket/avalanche-db-snapshot.tar.gz
```



## Testing Strategy

1. **Linting**: Run `./bin/run_task.sh lint-all` to ensure code quality

2. **Testing with MinIO and Bootstrap DB**:

   First, create a test database using the existing `initBootstrapDB` pattern:

   ```bash
   # Step 1: Create a utility to generate test DB (similar to tests/antithesis/init_db.go)
   # This requires adding a new tmpnetctl command to support LocalNetworkOrPanic
   ./bin/tmpnetctl init-local-network --bootstrap-db-path=./test-db
   ```

   Note: This requires implementing a new `init-local-network` command in tmpnetctl that:
   - Uses `LocalNetworkOrPanic()` from the tmpnet package to ensure consistent genesis
   - Calls `initBootstrapDB` to create the initial database state
   - Outputs the database to the specified path

   Then use MinIO for testing:

   ```bash
   # Step 2: Start MinIO in Docker
   docker run -d \
     --name minio-test \
     -p 9000:9000 \
     -p 9001:9001 \
     -e "MINIO_ROOT_USER=minioadmin" \
     -e "MINIO_ROOT_PASSWORD=minioadmin" \
     minio/minio server /data --console-address ":9001"

   # Step 3: Install MinIO client (mc)
   wget https://dl.min.io/client/mc/release/linux-amd64/mc
   chmod +x mc
   sudo mv mc /usr/local/bin/

   # Step 4: Configure MinIO client
   mc alias set myminio http://localhost:9000 minioadmin minioadmin

   # Step 5: Create test bucket
   mc mb myminio/test-bucket

   # Step 6: Create archive from bootstrap DB
   tar -czf test-db.tar.gz -C test-db .

   # Step 7: Upload to MinIO
   mc cp test-db.tar.gz myminio/test-bucket/

   # Step 8: Test the implementation
   ./bin/tmpnetctl start-local-network \
     --runtime=kube \
     --kube-initial-db-archive=s3://localhost:9000/test-bucket/test-db.tar.gz

   # Note: You may need to configure the download job to skip certificate
   # verification for localhost testing, or use the MinIO container's IP address
   ```

   **Additional Testing Considerations**:
   - The download job's alpine container will need to be able to reach MinIO
   - Consider using the MinIO container's cluster IP if running in the same Kubernetes cluster
   - For CI/CD, MinIO can be deployed as a Kubernetes service alongside the test

## Security Considerations

1. **Archive Validation**: Consider adding checksum verification
2. **Access Control**: Ensure S3 URLs use appropriate authentication
3. **Resource Limits**: Set appropriate resource limits on download job

## Performance Optimizations

1. **Parallel Extraction**: Use `pigz` for parallel gzip decompression
2. **Streaming Download**: Pipe download directly to extraction to save disk space
3. **Progress Reporting**: Add progress indicators for large downloads

## Future Enhancements

1. Support for different compression formats (bz2, xz, zstd)
2. Incremental updates instead of full DB replacement
3. Support for multiple DB snapshots (e.g., different network states)
4. Automatic cleanup of old shared PVCs

## Documentation Updates

1. Update README with usage examples
2. Document S3 bucket configuration requirements
3. Add troubleshooting guide for common issues
4. Include performance benchmarks comparing empty vs. pre-populated DB startup times
