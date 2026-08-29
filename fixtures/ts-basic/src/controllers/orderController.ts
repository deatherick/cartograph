import { OrderService } from "../services/orderService";
import { ValidationError } from "../utils/validation";
import { OrderLine } from "../models/order";

interface Request {
  body: unknown;
  params: Record<string, string>;
}

interface Response {
  status(code: number): Response;
  json(body: unknown): void;
}

export class OrderController {
  constructor(private orderService: OrderService) {}

  place(req: Request, res: Response): void {
    try {
      const body = req.body as { userId: string; lines: OrderLine[] };
      const order = this.orderService.placeOrder(body.userId, body.lines);
      res.status(201).json({ id: order.id, status: order.status });
    } catch (err) {
      if (err instanceof ValidationError) {
        res.status(400).json({ field: err.field, message: err.message });
        return;
      }
      throw err;
    }
  }

  markPaid(req: Request, res: Response): void {
    const order = this.orderService.markPaid(req.params.id);
    res.status(200).json({ id: order.id, status: order.status });
  }

  describe(req: Request, res: Response): void {
    const description = this.orderService.describeOrder(req.params.id);
    res.status(200).json({ description });
  }
}
