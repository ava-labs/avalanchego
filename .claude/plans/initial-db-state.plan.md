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
    kubeDocPrefix+"URL or path to the initial database archive (tar.gz format) to download before starting nodes",
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
```

### 3. Create Shared PVC for Database Archive

Add a new method in `kube_runtime.go` to create a shared PVC:
```go
func (p *KubeRuntime) createSharedDBPVC(ctx context.Context) (string, error) {
    pvcName := fmt.Sprintf("%s-shared-db", p.node.network.UUID)
    
    pvc := &corev1.PersistentVolumeClaim{
        ObjectMeta: metav1.ObjectMeta{
            Name:      pvcName,
            Namespace: p.runtimeConfig().Namespace,
        },
        Spec: corev1.PersistentVolumeClaimSpec{
            AccessModes: []corev1.PersistentVolumeAccessMode{
                corev1.ReadOnlyMany, // Allow multiple pods to read
            },
            Resources: corev1.VolumeResourceRequirements{
                Requests: corev1.ResourceList{
                    corev1.ResourceStorage: resource.MustParse("200Gi"), // Accommodate large DBs
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

### 6. Update Start Method (kube_runtime.go)

Modify the `Start()` method to handle initial DB setup:
```go
func (p *KubeRuntime) Start(ctx context.Context) error {
    // ... existing code ...
    
    // Check if this is the first node and if initial DB is configured
    var sharedDBPVCName string
    if p.node.network.isFirstNode(p.node) && len(runtimeConfig.InitialDBArchive) > 0 {
        // Create shared PVC
        pvcName, err := p.createSharedDBPVC(ctx)
        if err != nil {
            return fmt.Errorf("failed to create shared DB PVC: %w", err)
        }
        sharedDBPVCName = pvcName
        
        // Create and wait for download job
        if err := p.createDBDownloadJob(ctx, pvcName, runtimeConfig.InitialDBArchive); err != nil {
            return fmt.Errorf("failed to download initial DB: %w", err)
        }
    } else if len(runtimeConfig.InitialDBArchive) > 0 {
        // For subsequent nodes, just reference the existing PVC
        sharedDBPVCName = fmt.Sprintf("%s-shared-db", p.node.network.UUID)
    }
    
    // Pass the shared PVC name to StatefulSet creation
    statefulSet := NewNodeStatefulSet(
        // ... existing parameters ...
        sharedDBPVCName, // New parameter
    )
    
    // ... rest of the method ...
}
```

### 7. Cleanup Logic

Add cleanup for the shared PVC when the network is destroyed:
```go
func (p *KubeRuntime) cleanupSharedResources(ctx context.Context) error {
    // Delete the shared DB PVC if it exists
    pvcName := fmt.Sprintf("%s-shared-db", p.node.network.UUID)
    // ... deletion logic ...
}
```

## Archive Format Specification

The archive should be a gzipped tar file containing the database directory structure:
```
db/
├── mainnet/
├── C/
├── P/
└── X/
```

## Usage Example

```bash
./bin/tmpnetctl start-network \
  --runtime=kube \
  --kube-initial-db-archive=s3://my-bucket/avalanche-db-snapshot.tar.gz
```

## Testing Strategy

1. **Unit Tests**:
   - Flag parsing validation
   - Configuration structure updates

2. **Integration Tests**:
   - Test with small test archive
   - Verify init container execution
   - Confirm sentinel file prevents re-initialization

3. **E2E Tests**:
   - Full network deployment with initial DB
   - Verify faster startup compared to empty DB
   - Test node restarts don't re-copy DB

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