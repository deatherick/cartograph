import { OrderRepository } from "../repositories/orderRepository";
import { UserRepository } from "../repositories/userRepository";
import { Order, OrderLine, orderTotalCents } from "../models/order";
import { ValidationError } from "../utils/validation";
import { formatCents } from "../utils/format";

export class OrderService {
  constructor(
    private orders: OrderRepository,
    private users: UserRepository
  ) {}

  placeOrder(userId: string, lines: OrderLine[]): Order {
    const user = this.users.findById(userId);
    if (!user) throw new ValidationError("userId", "user not found");
    if (lines.length === 0) throw new ValidationError("lines", "order must have at least one line");

    const order: Order = {
      id: crypto.randomUUID(),
      userId,
      lines,
      status: "pending",
      createdAt: new Date(),
    };
    this.orders.insert(order);
    return order;
  }

  markPaid(orderId: string): Order {
    const order = this.orders.updateStatus(orderId, "paid");
    if (!order) throw new ValidationError("orderId", "order not found");
    return order;
  }

  describeOrder(orderId: string): string {
    const order = this.orders.findById(orderId);
    if (!order) throw new ValidationError("orderId", "order not found");
    return `Order ${order.id}: ${formatCents(orderTotalCents(order))} (${order.status})`;
  }

  ordersForUser(userId: string): Order[] {
    return this.orders.findByUserId(userId);
  }
}
