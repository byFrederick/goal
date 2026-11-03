package domain

// NotificationSettings are an organization's notification settings (ADR 0037 decision 14).
type NotificationSettings struct {
	// SlackSecret is the name of the secret that holds the Slack bot token; empty means Slack is
	// off, and the outbox and the digests queue nothing for Slack.
	SlackSecret string
}

// AuditOrgNotificationsUpdated is the audit action of a change of the organization's notification
// settings, targeting the organization (AuditTargetOrg). Its data holds the Slack secret's name
// (AuditSlackSecret; "" when Slack is turned off), never its value.
const AuditOrgNotificationsUpdated = "org.notifications_updated"

// AuditSlackSecret is the audit data key of the Slack secret's name.
const AuditSlackSecret = "slack_secret"
