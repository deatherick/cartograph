using System.Collections.Generic;
using OrderSystem.Models;
using OrderSystem.Repositories;
using OrderSystem.Utils;

namespace OrderSystem.Services;

public class OrderService
{
    private readonly IOrderRepository _orderRepository;

    public OrderService(IOrderRepository orderRepository)
    {
        _orderRepository = orderRepository;
    }

    public Order PlaceOrder(int id, int userId, List<OrderLine> lines)
    {
        foreach (var line in lines)
        {
            Validator.RequirePositive(line.Quantity, "quantity");
        }
        var order = new Order(id, userId, lines);
        _orderRepository.Save(order);
        return order;
    }

    public Order GetOrder(int id)
    {
        return _orderRepository.FindById(id);
    }
}
