import { createTransport } from 'nodemailer';
import type { Mail, Mailer } from './mailer.js';

/**
 * SMTPMailer sends through the server at url (GOAL_SMTP_URL: Mailpit locally, Amazon SES's SMTP
 * endpoint in the cloud) as from (GOAL_SMTP_FROM). smtps:// is TLS from the start; smtp:// must
 * upgrade with STARTTLS unless plaintext (settings allow it for loopback and, with
 * GOAL_SMTP_PLAINTEXT=on, a compose service), so credentials and codes never cross a network in
 * the clear. Timeouts keep a stuck server from holding a send for minutes.
 */
export class SMTPMailer implements Mailer {
  private readonly transport;

  constructor({ url, from, plaintext }: { url: string; from: string; plaintext: boolean }) {
    const u = new URL(url);
    const secure = u.protocol === 'smtps:';
    this.transport = createTransport(
      {
        host: u.hostname.replace(/^\[(.*)\]$/, '$1'),
        port: Number(u.port || (secure ? 465 : 25)),
        secure,
        requireTLS: !secure && !plaintext,
        ...(u.username ? { auth: { user: decodeURIComponent(u.username), pass: decodeURIComponent(u.password) } } : {}),
        connectionTimeout: 10_000,
        greetingTimeout: 10_000,
        socketTimeout: 30_000,
        disableFileAccess: true,
        disableUrlAccess: true,
      },
      { from },
    );
  }

  async send(m: Mail): Promise<void> {
    await this.transport.sendMail({ to: m.to, subject: m.subject, text: m.text, html: m.html });
  }

  close(): void {
    this.transport.close();
  }
}
