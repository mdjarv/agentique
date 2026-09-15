import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Markdown } from "~/components/chat/Markdown";
import { SessionMachineContext } from "~/components/chat/SessionMachineContext";

vi.mock("~/lib/machines/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("~/lib/machines/api")>();
  return {
    ...actual,
    rewriteRemoteLocalhost: (href: string, machineId: string | null) =>
      machineId ? href.replace("localhost", "zbook.tail1.ts.net") : href,
  };
});

const SESSION = "11111111-2222-4333-8444-555555555555";

afterEach(cleanup);

function renderOnRemote(content: string) {
  return render(
    <SessionMachineContext.Provider value="zbook">
      <Markdown content={content} />
    </SessionMachineContext.Provider>,
  );
}

// A link an agent on a paired machine writes resolves against this page's
// server, which relays it; it is never sent to the machine's own host.
describe("session file links in a remote session", () => {
  it("keeps a relative non-markdown link relative", () => {
    renderOnRemote(`[report](/api/sessions/${SESSION}/files/report.pdf)`);
    expect(screen.getByRole("link", { name: "report" })).toHaveAttribute(
      "href",
      `/api/sessions/${SESSION}/files/report.pdf`,
    );
  });

  it("normalises an absolute-localhost session link instead of rewriting its host", () => {
    renderOnRemote(`[report](http://localhost:9201/api/sessions/${SESSION}/files/report.pdf)`);
    expect(screen.getByRole("link", { name: "report" })).toHaveAttribute(
      "href",
      `/api/sessions/${SESSION}/files/report.pdf`,
    );
  });

  it("still rewrites a localhost link that is not a session file", () => {
    renderOnRemote("[app](http://localhost:3000/dev)");
    expect(screen.getByRole("link", { name: "app" })).toHaveAttribute(
      "href",
      "http://zbook.tail1.ts.net:3000/dev",
    );
  });

  it("opens an absolute-localhost markdown file in the preview, not as a link", () => {
    renderOnRemote(`[notes](http://localhost:9201/api/sessions/${SESSION}/files/notes.md)`);
    expect(screen.queryByRole("link", { name: "notes" })).toBeNull();
    expect(screen.getByRole("button", { name: "notes" })).toBeInTheDocument();
  });
});
