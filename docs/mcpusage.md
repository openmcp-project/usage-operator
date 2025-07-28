# MCPUsage Resource

The `MCPUsage` resource is created automatically by the usage-operator based on the MCPs located on the onboarding cluster.
The resources get a UUID as there name, but also contain the project, workspace and mcp-name. This is also visible in the display columns, when running `kubectl get mcpusage`.

The resource is structured like so:

```yaml
apiVersion: usage.openmcp.cloud/v1
kind: MCPUsage
metadata:
  name: 0fde12fa-c822-5d51-a2c2-aa11be641f0d
spec:
  project: test
  workspace: test
  mcp: test-bug
  charging_target: missing
  charging_target_type: ""
  daily_usage:
  - date: "2025-07-22T00:00:00Z"
    usage: 14h57m21.929782431s
  - date: "2025-07-23T00:00:00Z"
    usage: 24h0m0s
  - date: "2025-07-24T00:00:00Z"
    usage: 24h0m0s
  - date: "2025-07-25T00:00:00Z"
    usage: 24h0m0s
  - date: "2025-07-26T00:00:00Z"
    usage: 24h0m0s
  - date: "2025-07-27T00:00:00Z"
    usage: 24h0m0s
  - date: "2025-07-28T00:00:00Z"
    usage: 6h0m4.499363869s
  last_usage_captured: "2025-07-28T06:52:32Z"
  mcp_created_at: "2025-07-22T09:07:12Z"
  message: no charging target specified
```

This is what the resource looks like, when the usage-operator creates and manages it, the status is untouched, as this is the responsibility of a `metering-operator` (see [Metering Operator](metering-operator.md))

## Garbage Collection

The `usage-operator` enforces a strict garbage collection policy for the `daily_usage` field, retaining usage data for the most recent **32** days only. This allows you to review usage status for up to one month. The garbage collection operates on a rolling basis, automatically removing the oldest entry each day to maintain the 32-day window.

There are plans to make the **32**-day retention window configurable in future releases of the `usage-operator`.
