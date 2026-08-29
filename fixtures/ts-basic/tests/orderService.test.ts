import { OrderService } from "../src/services/orderService";
import { OrderRepository } from "../src/repositories/orderRepository";
import { UserRepository } from "../src/repositories/userRepository";

describe("OrderService", () => {
  function setup() {
    const orders = new OrderRepository();
    const users = new UserRepository();
    const user = users.insert({ email: "a@b.com", name: "Ada", password: "x" }, "hash");
    return { service: new OrderService(orders, users), user };
  }

  it("places an order for an existing user", () => {
    const { service, user } = setup();
    const order = service.placeOrder(user.id, [{ sku: "sku1", quantity: 2, unitPriceCents: 500 }]);
    expect(order.status).toBe("pending");
  });

  it("rejects an order with no lines", () => {
    const { service, user } = setup();
    expect(() => service.placeOrder(user.id, [])).toThrow();
  });

  it("marks an order as paid", () => {
    const { service, user } = setup();
    const order = service.placeOrder(user.id, [{ sku: "sku1", quantity: 1, unitPriceCents: 100 }]);
    const paid = service.markPaid(order.id);
    expect(paid.status).toBe("paid");
  });
});
