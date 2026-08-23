import { beforeEach, describe, expect, it, vi } from "vitest";
import { machineFetch } from "~/lib/machines/registry";
import { useMachineStore } from "~/stores/machine-store";

const MACHINE_ID = "20000000-0000-4000-8000-000000000002";

beforeEach(() => {
  useMachineStore.setState({
    machines: {
      [MACHINE_ID]: {
        machineId: MACHINE_ID,
        label: "remote",
        baseUrl: "https://remote.example",
        token: "long-lived-secret",
        sessionId: "remote-session",
        identityKey: "pinned-public-key",
        addedAt: "2026-08-23T00:00:00Z",
      },
    },
    statuses: {},
    faults: {},
  });
});

describe("machineFetch identity admission", () => {
  it("does not persist remote bearer tokens in localStorage", () => {
    const persisted = localStorage.getItem("agentique:machines");
    expect(persisted).not.toContain("long-lived-secret");
  });

  it("does not disclose the bearer when identity proof is unavailable", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/.well-known/agentique/environment")) {
        return Response.json({ machineId: MACHINE_ID, identityKey: "pinned-public-key" });
      }
      if (url.endsWith("/api/auth/identity-proof")) {
        return new Response(null, { status: 503 });
      }
      return Response.json([]);
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(machineFetch(MACHINE_ID, "/api/projects")).rejects.toThrow(/identity/i);

    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/api/projects"))).toBe(
      false,
    );
    for (const [, init] of fetchMock.mock.calls) {
      expect(new Headers(init?.headers).has("Authorization")).toBe(false);
    }
  });
});
