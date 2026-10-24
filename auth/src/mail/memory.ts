import type { Mail, Mailer } from './mailer.js';

/** MemoryMailer keeps what it is sent, for tests; delay makes each send take that long. */
export class MemoryMailer implements Mailer {
  readonly sent: Mail[] = [];

  constructor(private readonly delayMs = 0) {}

  async send(m: Mail): Promise<void> {
    if (this.delayMs > 0) await new Promise((resolve) => setTimeout(resolve, this.delayMs));
    this.sent.push(m);
  }

  /** to lists the messages sent to address, oldest first. */
  to(address: string): Mail[] {
    return this.sent.filter((m) => m.to === address);
  }
}
