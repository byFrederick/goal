---
name: incident-triage
description: Classify and size SRE work items (incident, change, toil, project), assign severity sev1-sev4, and decide whether to split. Use when triaging an alert, ticket or request in the SRE factory.
---

# Incident triage

## Severity
| Severity | Meaning |
| --- | --- |
| sev1 | Customer-facing outage or data loss in production |
| sev2 | Degraded production service, or a sev1 risk within hours |
| sev3 | Non-urgent production issue, or staging blocked |
| sev4 | Toil, hygiene, questions |

## Sizing
- S: one service, one hypothesis, under 30 minutes of investigation.
- M: one or two services, a few hypotheses.
- L: several services or an open-ended question. Split it.

## Always
- Search the knowledge base for the error string, the service name and the alert name.
- Link duplicates instead of re-investigating them.
