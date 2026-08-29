export interface OrderLine {
  sku: string;
  quantity: number;
  unitPriceCents: number;
}

export interface Order {
  id: string;
  userId: string;
  lines: OrderLine[];
  status: "pending" | "paid" | "shipped" | "cancelled";
  createdAt: Date;
}

export function orderTotalCents(order: Order): number {
  return order.lines.reduce((sum, line) => sum + line.quantity * line.unitPriceCents, 0);
}
