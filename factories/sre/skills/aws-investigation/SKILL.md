---
name: aws-investigation
description: Read-only AWS investigation playbook for ECS, RDS, ALB, Lambda and CloudWatch issues. Use when gathering evidence from an AWS account during an SRE investigation.
---

# AWS investigation (read-only)

Credentials come from the `aws-readonly` connection; never ask for or print keys.

1. Confirm identity and region: `aws sts get-caller-identity`.
2. Scope the blast radius: CloudWatch alarms in ALARM state, then the affected service's metrics for the incident window.
3. ECS: `aws ecs describe-services` (deployments, events), stopped task reasons via `describe-tasks`.
4. Logs: CloudWatch Logs Insights over the narrowest window and log group that can confirm a hypothesis.
5. Recent changes: CloudTrail `lookup-events` for the resource, and deploys in GitHub for the same window.

Record each command and the line of output that matters as evidence.
