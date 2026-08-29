import { Order } from "../models/order";

export class OrderRepository {
  private orders = new Map<string, Order>();

  findById(id: string): Order | undefined {
    return this.orders.get(id);
  }

  findByUserId(userId: string): Order[] {
    return Array.from(this.orders.values()).filter((o) => o.userId === userId);
  }

  insert(order: Order): void {
    this.orders.set(order.id, order);
  }

  updateStatus(id: string, status: Order["status"]): Order | undefined {
    const order = this.orders.get(id);
    if (!order) return undefined;
    order.status = status;
    return order;
  }
}
