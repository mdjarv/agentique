import { beforeEach, describe, expect, it } from "vitest";
import {
  machineIdForProject,
  rewriteRemoteLocalhost,
  sessionFileMachineId,
} from "~/lib/machines/api";
import { useAppStore } from "~/stores/app-store";
import type { SessionMetadata } from "~/stores/chat-store";
import { useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";

const ZBOOK = "ad3eb932-0000-4000-8000-000000000000";
const SESSION = "11111111-2222-4333-8444-555555555555";
const PROJECT = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee";

beforeEach(() => {
  useMachineStore.setState({
    machines: {
      [ZBOOK]: {
        machineId: ZBOOK,
        label: "zbook",
        baseUrl: "https://zbook.tail1.ts.net:19201",
        token: "t",
        addedAt: "",
      },
    },
    statuses: {},
  });
  useAppStore.setState({
    projects: [{ id: PROJECT, slug: "x", name: "x", path: "/x", machineId: ZBOOK } as never],
  });
  useChatStore.setState({
    sessions: {
      [SESSION]: { meta: { id: SESSION, projectId: PROJECT } as SessionMetadata } as never,
    },
  });
});

describe("rewriteRemoteLocalhost", () => {
  it("rewrites localhost and 127.0.0.1 to the machine's host, keeping port/path/scheme", () => {
    expect(rewriteRemoteLocalhost("http://localhost:9210/dev/agents", ZBOOK)).toBe(
      "http://zbook.tail1.ts.net:9210/dev/agents",
    );
    expect(rewriteRemoteLocalhost("http://127.0.0.1:3000/?a=1", ZBOOK)).toBe(
      "http://zbook.tail1.ts.net:3000/?a=1",
    );
  });

  it("leaves non-localhost, relative, and primary-session links alone", () => {
    expect(rewriteRemoteLocalhost("https://example.com/x", ZBOOK)).toBe("https://example.com/x");
    expect(rewriteRemoteLocalhost("/api/sessions/x/files/a.png", ZBOOK)).toBe(
      "/api/sessions/x/files/a.png",
    );
    expect(rewriteRemoteLocalhost("http://localhost:9210/", null)).toBe("http://localhost:9210/");
  });

  it("leaves links alone for unknown machines", () => {
    expect(rewriteRemoteLocalhost("http://localhost:9210/", "not-a-machine")).toBe(
      "http://localhost:9210/",
    );
  });
});

describe("sessionFileMachineId", () => {
  it("resolves a remote session's file URL to its machine", () => {
    expect(sessionFileMachineId(`/api/sessions/${SESSION}/files/shot.png`)).toBe(ZBOOK);
  });

  it("tolerates the absolute localhost form agents sometimes write", () => {
    expect(sessionFileMachineId(`http://localhost:19201/api/sessions/${SESSION}/files/a.png`)).toBe(
      ZBOOK,
    );
    expect(sessionFileMachineId(`http://127.0.0.1:9201/api/sessions/${SESSION}/files/a.png`)).toBe(
      ZBOOK,
    );
  });

  it("returns undefined for unknown sessions, foreign origins, and non-file paths", () => {
    expect(
      sessionFileMachineId("/api/sessions/99999999-0000-4000-8000-000000000000/files/a.png"),
    ).toBeUndefined();
    expect(
      sessionFileMachineId(`https://elsewhere.example/api/sessions/${SESSION}/files/a.png`),
    ).toBeUndefined();
    expect(sessionFileMachineId(`/api/sessions/${SESSION}/history`)).toBeUndefined();
  });
});

describe("machineIdForProject", () => {
  it("resolves only projects tagged with a still-paired machine", () => {
    expect(machineIdForProject(PROJECT)).toBe(ZBOOK);
    useMachineStore.setState({ machines: {}, statuses: {} });
    expect(machineIdForProject(PROJECT)).toBeUndefined();
  });
});
