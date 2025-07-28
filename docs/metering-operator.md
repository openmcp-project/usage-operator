# Metering Operators

As metering the usage of your platform to your customers is highly dependend on your environment, we created a system which decouples usage collection from the actual metering. The [MCPUsage](mcpusage.md) resource is the connection point.
This resource just reports the usage of your platform. The metering itself needs a custom operator, you need to provide yourself.

A metering operator needs to reconcile the `MCPUsage` resource and extracts the usage information from the `spec`. To report status back, it can edit the `status` of the respective `MCPUsage` resource.
One example is the following resource:

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
status:
  daily_usage_report:
  - date: "2025-07-22T00:00:00Z"
    message: charging target missing
    status: Failed
  - date: "2025-07-23T00:00:00Z"
    message: charging target missing
    status: Failed
  - date: "2025-07-24T00:00:00Z"
    message: charging target missing
    status: Failed
  - date: "2025-07-25T00:00:00Z"
    message: charging target missing
    status: Failed
  - date: "2025-07-26T00:00:00Z"
    message: charging target missing
    status: Failed
  - date: "2025-07-27T00:00:00Z"
    message: charging target missing
    status: Failed
```

As you can see, the metering operator can report a list of `daily_usage_report` back, to provide information for every day the usage is collected.
It can provice a short status and a message. In the example, the responsible metering operator reports, that the charging target is missing.

As the status is not used for anything in the usage-operator, you can decide what messages you want to display there. This can be used for your metering operator to check, which usage entry it already reported, and what maybe needs to be reported again. Keep in mind, that the status of the resource is not permanently stored and can be lost, due to kubernetes own guidelines. So your operator should not depend on the status being saved indefinitely.
