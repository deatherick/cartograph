using System.Collections.Generic;
using OrderSystem.Models;

namespace OrderSystem.Repositories;

public class OrderRepository : IOrderRepository
{
    private readonly List<Order> _orders = new List<Order>();

    public void Save(Order order)
    {
        _orders.Add(order);
    }

    public Order FindById(int id)
    {
        foreach (var order in _orders)
        {
            if (order.Id == id)
            {
                return order;
            }
        }
        return null;
    }
}
