using OrderSystem.Models;
using OrderSystem.Services;

namespace OrderSystem.Controllers;

public class UserController
{
    private readonly UserService _userService;

    public UserController(UserService userService)
    {
        _userService = userService;
    }

    public User Register(int id, string email, string name)
    {
        return _userService.Register(id, email, name);
    }

    public void PromoteToAdmin(int userId)
    {
        _userService.PromoteToAdmin(userId);
    }
}
