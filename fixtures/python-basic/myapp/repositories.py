class UserRepository:
    def __init__(self):
        self.users = []

    def find_by_email(self, email):
        for user in self.users:
            if user.email == email:
                return user
        return None

    def find_by_id(self, user_id):
        for user in self.users:
            if user.id == user_id:
                return user
        return None

    def save(self, user):
        self.users.append(user)


class OrderRepository:
    def __init__(self):
        self.orders = []

    def find_by_id(self, order_id):
        for order in self.orders:
            if order.id == order_id:
                return order
        return None

    def save(self, order):
        self.orders.append(order)
