using OrderSystem.Models;

namespace OrderSystem.Repositories;

public interface IOrderRepository
{
    void Save(Order order);
    Order FindById(int id);
}
