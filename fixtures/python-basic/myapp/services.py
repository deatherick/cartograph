from .models import User, Order
from .repositories import UserRepository, OrderRepository
from .utils import require_positive, ValidationError


class UserService:
    def __init__(self, user_repository):
        self.user_repository = UserRepository()

    def register(self, user_id, email, name):
        existing = self.user_repository.find_by_email(email)
        if existing is not None:
            raise ValidationError("email already registered")
        user = User(user_id, email, name)
        self.user_repository.save(user)
        return user

    def promote_to_admin(self, user_id):
        user = self.user_repository.find_by_id(user_id)
        user.promote_to_admin()
        self.user_repository.save(user)


class OrderService:
    def __init__(self, order_repository):
        self.order_repository = OrderRepository()

    def place_order(self, order_id, user_id, lines):
        for line in lines:
            require_positive(line.quantity, "quantity")
        order = Order(order_id, user_id, lines)
        self.order_repository.save(order)
        return order

    def get_order(self, order_id):
        return self.order_repository.find_by_id(order_id)
