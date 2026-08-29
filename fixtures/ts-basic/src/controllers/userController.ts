import { UserService } from "../services/userService";
import { ValidationError } from "../utils/validation";

interface Request {
  body: unknown;
  params: Record<string, string>;
}

interface Response {
  status(code: number): Response;
  json(body: unknown): void;
}

export class UserController {
  constructor(private userService: UserService) {}

  register(req: Request, res: Response): void {
    try {
      const input = req.body as { email: string; name: string; password: string };
      const user = this.userService.register(input);
      res.status(201).json({ id: user.id, email: user.email, name: user.name });
    } catch (err) {
      if (err instanceof ValidationError) {
        res.status(400).json({ field: err.field, message: err.message });
        return;
      }
      throw err;
    }
  }

  get(req: Request, res: Response): void {
    const user = this.userService.getById(req.params.id);
    if (!user) {
      res.status(404).json({ message: "not found" });
      return;
    }
    res.status(200).json({ id: user.id, email: user.email, name: user.name });
  }

  list(_req: Request, res: Response): void {
    const users = this.userService.listAll();
    res.status(200).json(users.map((u) => ({ id: u.id, email: u.email, name: u.name })));
  }
}
