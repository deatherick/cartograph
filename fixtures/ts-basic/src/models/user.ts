export interface User {
  id: string;
  email: string;
  name: string;
  passwordHash: string;
  role: "admin" | "member";
  createdAt: Date;
}

export interface CreateUserInput {
  email: string;
  name: string;
  password: string;
}

export function isAdmin(user: User): boolean {
  return user.role === "admin";
}
