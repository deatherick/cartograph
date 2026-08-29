import { UserService } from "../src/services/userService";
import { UserRepository } from "../src/repositories/userRepository";
import { EmailService } from "../src/services/emailService";

describe("UserService", () => {
  function setup() {
    const repo = new UserRepository();
    const email = new EmailService();
    return { service: new UserService(repo, email), repo, email };
  }

  it("registers a user and sends a welcome email", () => {
    const { service, email } = setup();
    const user = service.register({ email: "a@b.com", name: "Ada", password: "secret" });
    expect(user.email).toBe("a@b.com");
    expect(email.sentMessages()).toHaveLength(1);
  });

  it("rejects duplicate email registration", () => {
    const { service } = setup();
    service.register({ email: "a@b.com", name: "Ada", password: "secret" });
    expect(() => service.register({ email: "a@b.com", name: "Ada2", password: "x" })).toThrow();
  });

  it("rejects an invalid email format", () => {
    const { service } = setup();
    expect(() => service.register({ email: "not-an-email", name: "Ada", password: "x" })).toThrow();
  });
});
