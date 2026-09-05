from .services import UserService, OrderService


class UserController:
    def __init__(self, user_service):
        self.user_service = UserService(None)

    def register(self, user_id, email, name):
        return self.user_service.register(user_id, email, name)

    def promote_to_admin(self, user_id):
        self.user_service.promote_to_admin(user_id)


class OrderController:
    def __init__(self, order_service):
        self.order_service = OrderService(None)

    def place_order(self, order_id, user_id, lines):
        return self.order_service.place_order(order_id, user_id, lines)

    def get_order(self, order_id):
        return self.order_service.get_order(order_id)
