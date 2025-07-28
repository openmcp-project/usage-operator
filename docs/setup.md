# Setup

To install the usage-operator into your MCP landscape, you need to register it as a new PlatformService.
This can be done using the `PlatformService` Resource.

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: PlatformService
metadata:
  name: usage-operator
spec:
  image: "ghcr.io/openmcp-project/images/usage-operator:v0.0.11"
  imagePullSecrets: []
```

The usage-operator will then automatically be installed by the mcp platform and requests its permissions for the onboarding cluster.
Inside the onboarding cluster a new CRD will then be installed called `MCPUsage`. This resource is completely managed by the `usage-operator` and there is no need to create resource manually.
