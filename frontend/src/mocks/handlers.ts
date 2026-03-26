import { http, HttpResponse } from "msw";
import { MOCK_PROJECTS } from "./data";

export const restHandlers = [
  http.get("/api/projects", () => {
    return HttpResponse.json(MOCK_PROJECTS);
  }),

  http.post("/api/projects", () => {
    return HttpResponse.json(MOCK_PROJECTS[0], { status: 201 });
  }),

  http.patch("/api/projects/:id", () => {
    return HttpResponse.json(MOCK_PROJECTS[0]);
  }),

  http.delete("/api/projects/:id", () => {
    return new HttpResponse(null, { status: 204 });
  }),

  http.get("/api/health", () => {
    return HttpResponse.json({ status: "ok" });
  }),
];
