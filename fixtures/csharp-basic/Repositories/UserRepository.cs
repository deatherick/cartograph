using System.Collections.Generic;
using OrderSystem.Models;

namespace OrderSystem.Repositories;

public class UserRepository : IUserRepository
{
    private readonly List<User> _users = new List<User>();

    public User FindByEmail(string email)
    {
        foreach (var user in _users)
        {
            if (user.Email == email)
            {
                return user;
            }
        }
        return null;
    }

    public User FindById(int id)
    {
        foreach (var user in _users)
        {
            if (user.Id == id)
            {
                return user;
            }
        }
        return null;
    }

    public void Save(User user)
    {
        _users.Add(user);
    }
}
