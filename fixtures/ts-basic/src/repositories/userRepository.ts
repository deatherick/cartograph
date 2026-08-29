import { User, CreateUserInput } from "../models/user";

export class UserRepository {
  private users = new Map<string, User>();

  findById(id: string): User | undefined {
    return this.users.get(id);
  }

  findByEmail(email: string): User | undefined {
    for (const user of this.users.values()) {
      if (user.email === email) return user;
    }
    return undefined;
  }

  insert(input: CreateUserInput, passwordHash: string): User {
    const user: User = {
      id: crypto.randomUUID(),
      email: input.email,
      name: input.name,
      passwordHash,
      role: "member",
      createdAt: new Date(),
    };
    this.users.set(user.id, user);
    return user;
  }

  all(): User[] {
    return Array.from(this.users.values());
  }
}
