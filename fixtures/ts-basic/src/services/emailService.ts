export interface EmailMessage {
  to: string;
  subject: string;
  body: string;
}

export class EmailService {
  private sent: EmailMessage[] = [];

  send(message: EmailMessage): void {
    // In production this would call an SMTP/API provider. For now we just
    // record it so tests and other services can assert on delivery.
    this.sent.push(message);
  }

  sentMessages(): EmailMessage[] {
    return this.sent;
  }
}

export function welcomeEmail(name: string, email: string): EmailMessage {
  return {
    to: email,
    subject: "Welcome!",
    body: `Hi ${name}, thanks for signing up.`,
  };
}
