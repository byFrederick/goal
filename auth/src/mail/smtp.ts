import { createTransport } from 'nodemailer';
import type { Mail, Mailer } from './mailer.js';

/**
 * SMTPMailer sends through the server at url (GOAL_SMTP_URL: Mailpit locally, Amazon SES's SMTP
 * endpoint in the cloud) as from (GOAL_SMTP_FROM). smtps:// is TLS from the start; smtp:// to a
 * host other than loopback must upgrade with STARTTLS, so credentials and codes never cross a
 * network in the clear. Timeouts keep a stuck server from holding a send for minutes.
 */
export class SMTPMailer implements Mailer {
  private readonly transport;

  constructor(url: string, from: string) {
    const u = new URL(url);
    const loopback = ['127.0.0.1', 'localhost', '[::1]'].includes(u.hostname);
    this.transport = createTransport(
      {
        host: u.hostname.replace(/^\[(.*)\]$/, '$1'),
        port: Number(u.port || (u.protocol === 'smtps:' ? 465 : 25)),
        secure: u.protocol === 'smtps:',
        requireTLS: u.protocol === 'smtp:' && !loopback,
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
