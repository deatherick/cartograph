namespace OrderSystem.Utils;

public class ValidationError : System.Exception
{
    public ValidationError(string message) : base(message)
    {
    }
}

public static class Validator
{
    public static void RequireNonNegative(int value, string fieldName)
    {
        if (value < 0)
        {
            throw new ValidationError(fieldName);
        }
    }

    public static void RequirePositive(int value, string fieldName)
    {
        if (value <= 0)
        {
            throw new ValidationError(fieldName);
        }
    }
}
