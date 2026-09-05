using OrderSystem.Models;
using OrderSystem.Repositories;
using OrderSystem.Utils;

namespace OrderSystem.Services;

public class UserService
{
    private readonly IUserRepository _userRepository;

    public UserService(IUserRepository userRepository)
    {
        _userRepository = userRepository;
    }

    public User Register(int id, string email, string name)
    {
        var existing = _userRepository.FindByEmail(email);
        if (existing != null)
        {
            throw new ValidationError("email already registered");
        }
        var user = new User(id, email, name);
        _userRepository.Save(user);
        return user;
    }

    public void PromoteToAdmin(int userId)
    {
        var user = _userRepository.FindById(userId);
        Validator.RequirePositive(userId, "userId");
        user.PromoteToAdmin();
        _userRepository.Save(user);
    }
}
