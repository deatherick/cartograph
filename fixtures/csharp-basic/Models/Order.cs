using System.Collections.Generic;

namespace OrderSystem.Models;

public class OrderLine
{
    public string Sku { get; set; }
    public int Quantity { get; set; }

    public OrderLine(string sku, int quantity)
    {
        Sku = sku;
        Quantity = quantity;
    }
}

public class Order
{
    public int Id { get; set; }
    public int UserId { get; set; }
    public List<OrderLine> Lines { get; set; }

    public Order(int id, int userId, List<OrderLine> lines)
    {
        Id = id;
        UserId = userId;
        Lines = lines;
    }

    public int TotalQuantity()
    {
        var total = 0;
        foreach (var line in Lines)
        {
            total += line.Quantity;
        }
        return total;
    }
}
