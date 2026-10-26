// The auth service's port for email (spec 3 §5.2): sign-in codes, invitations and the lockout
// notice. Adapters: smtp.ts (Mailpit locally, Amazon SES SMTP in the cloud) and memory.ts (tests).
// This folder imports nothing else of the service.

/** Mail is one message to one address: plain text plus minimal HTML, no remote content. */
export interface Mail {
  to: string;
  subject: string;
  text: string;
  html: string;
}

export interface Mailer {
  /** send delivers m or rejects; it never logs the message, which may hold a sign-in code. */
  send(m: Mail): Promise<void>;
}
