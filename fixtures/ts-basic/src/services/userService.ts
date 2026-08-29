import { UserRepository } from "../repositories/userRepository";
import { EmailService, welcomeEmail } from "./emailService";
import { CreateUserInput, User, isAdmin } from "../models/user";
import { assertValidEmail, isNonEmpty, ValidationError } from "../utils/validation";

function hashPassword(password: string): string {
  // placeholder — a real implementation would use bcrypt/argon2
  return `hashed:${password}`;
}

export class UserService {
  constructor(
    private repo: UserRepository,
    private emailService: EmailService
  ) {}

  register(input: CreateUserInput): User {
    assertValidEmail(input.email);
    if (!isNonEmpty(input.name)) {
      throw new ValidationError("name", "name is required");
    }
    if (this.repo.findByEmail(input.email)) {
      throw new ValidationError("email", "email already registered");
    }

    const user = this.repo.insert(input, hashPassword(input.password));
    this.emailService.send(welcomeEmail(user.name, user.email));
    return user;
  }

  getById(id: string): User | undefined {
    return this.repo.findById(id);
  }

  promoteToAdmin(id: string): User {
    const user = this.repo.findById(id);
    if (!user) throw new ValidationError("id", "user not found");
    if (isAdmin(user)) return user;
    user.role = "admin";
    return user;
  }

  listAll(): User[] {
    return this.repo.all();
  }
}
