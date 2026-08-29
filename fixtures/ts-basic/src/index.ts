import { UserRepository } from "./repositories/userRepository";
import { OrderRepository } from "./repositories/orderRepository";
import { EmailService } from "./services/emailService";
import { UserService } from "./services/userService";
import { OrderService } from "./services/orderService";
import { UserController } from "./controllers/userController";
import { OrderController } from "./controllers/orderController";

const userRepo = new UserRepository();
const orderRepo = new OrderRepository();
const emailService = new EmailService();

const userService = new UserService(userRepo, emailService);
const orderService = new OrderService(orderRepo, userRepo);

export const userController = new UserController(userService);
export const orderController = new OrderController(orderService);
