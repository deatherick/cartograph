namespace OrderSystem.Models;

public class User
{
    public int Id { get; set; }
    public string Email { get; set; }
    public string Name { get; set; }
    public bool IsAdmin { get; set; }

    public User(int id, string email, string name)
    {
        Id = id;
        Email = email;
        Name = name;
        IsAdmin = false;
    }

    public void PromoteToAdmin()
    {
        IsAdmin = true;
    }
}
