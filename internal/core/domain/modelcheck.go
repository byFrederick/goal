package domain

// Limits of the model check (ADR 0028 decision 3).
const (
	// MaxModelQuestion is the most characters an operator's question has.
	MaxModelQuestion = 500
	// MaxModelReason is the most characters of the model's reason a firing keeps.
	MaxModelReason = 200
)

// ModelAgrees is a trigger's model check, the last of its only: conditions (ADR 0028 decision
// 3): the event passes when a model answers yes to Question about it.
type ModelAgrees struct {
	// Question is the operator's yes or no question, trimmed, at most MaxModelQuestion characters.
	Question string
	// Connection is the organization connection that pays for the check; "" is the factory's
	// runtime.modelConnection.
	Connection string
	// Model is the model asked; "" leaves it to the adapter's default for the connection's kind.
	Model string
}

// ModelQuestion is what ports.ModelCheck asks a model: the operator's question about an event.
// The event comes from outside the organization: an adapter puts it in front of the model only as
// untrusted data, never as instructions (ADR 0028 decision 5).
type ModelQuestion struct {
	// Model is the model asked; "" is the adapter's default for the connection's kind.
	Model    string
	Question string
	Event    IntakeEvent
}

// ModelAnswer is a model's structured answer to a ModelQuestion and what it cost.
type ModelAnswer struct {
	// Worth is yes: the event is worth an item.
	Worth bool
	// Reason is the model's one-line reason; it is shaped by the event, so it is untrusted text.
	Reason string
	// Model is the model that answered.
	Model                     string
	InputTokens, OutputTokens int
	CostUSD                   float64
}
