import { startDemo } from "./worker";

startDemo().then(() => import("../main"));
