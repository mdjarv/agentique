import { setupWorker } from "msw/browser";
import { restHandlers } from "./handlers";
import { wsHandler } from "./ws-handler";

export const worker = setupWorker(...restHandlers, wsHandler);
