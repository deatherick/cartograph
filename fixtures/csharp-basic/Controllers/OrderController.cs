using System.Collections.Generic;
using OrderSystem.Models;
using OrderSystem.Services;

namespace OrderSystem.Controllers;

public class OrderController
{
    private readonly OrderService _orderService;

    public OrderController(OrderService orderService)
    {
        _orderService = orderService;
    }

    public Order PlaceOrder(int id, int userId, List<OrderLine> lines)
    {
        return _orderService.PlaceOrder(id, userId, lines);
    }

    public Order GetOrder(int id)
    {
        return _orderService.GetOrder(id);
    }
}
