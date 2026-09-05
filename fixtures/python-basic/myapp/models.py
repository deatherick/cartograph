class User:
    def __init__(self, user_id, email, name):
        self.id = user_id
        self.email = email
        self.name = name
        self.is_admin = False

    def promote_to_admin(self):
        self.is_admin = True


class OrderLine:
    def __init__(self, sku, quantity):
        self.sku = sku
        self.quantity = quantity


class Order:
    def __init__(self, order_id, user_id, lines):
        self.id = order_id
        self.user_id = user_id
        self.lines = lines

    def total_quantity(self):
        total = 0
        for line in self.lines:
            total += line.quantity
        return total
