using OrderSystem.Models;

namespace OrderSystem.Repositories;

public interface IUserRepository
{
    User FindByEmail(string email);
    User FindById(int id);
    void Save(User user);
}
