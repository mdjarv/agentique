/**
 * A heartbeat tick lands in the conversation as a PAIR, and the pair renders as
 * two different things: the server's note is a divider (nobody said it), the
 * head's reply an ordinary bubble wearing a small mark.
 *
 * The divider is what these assertions are for. Rendered as a bubble it would
 * put the server's words in the assistant's voice, and rendered as nothing at
 * all the conversation would show a reply to a question that is not there.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantConversation } from "~/components/assistant/AssistantConversation";
import { buildTimeline } from "~/lib/assistant/timeline";
import type { AssistantJournalEntry, AssistantMessage } from "~/lib/assistant/wire";

// The markdown renderer is a whole pipeline and not the subject.
vi.mock("~/components/chat/Markdown", () => ({
  Markdown: ({ content }: { content: string }) => <p>{content}</p>,
}));

vi.mock("~/components/assistant/use-session-label", () => ({
  useSessionLabel: (id?: string) => (id ? "Fix the loader" : undefined),
}));

afterEach(cleanup);

function asItems(messages: AssistantMessage[], journal: AssistantJournalEntry[] = []) {
  return buildTimeline({ messages, journal, proposals: [], unseenOnArrival: 0 });
}

// The shape the server actually writes: the verdict sentence on the first line,
// then the window the head was woken with. A fixture of one line hid the bug
// this file is now the guard for — the divider drew all of it.
const VERDICT =
  'The heartbeat woke the assistant. Triage answered, as data: "keep the tests green"';
const WINDOW_LINE = "the retry fix finished -- the tests pass";
const NOTICE: AssistantMessage = {
  id: "m1",
  role: "system",
  kind: "heartbeat",
  text: `${VERDICT}\n\nWhat has happened since the last check, oldest first.\n\n- ${WINDOW_LINE}`,
  createdAt: "2026-09-12T08:15:00.000000000Z",
};

const REPLY: AssistantMessage = {
  id: "m2",
  role: "assistant",
  kind: "heartbeat",
  text: "Started a session on the failing tests.",
  createdAt: "2026-09-12T08:15:20.000000000Z",
};

describe("AssistantConversation", () => {
  it("renders the heartbeat's note as a quiet divider with its sentence and time", () => {
    render(<AssistantConversation items={asItems([NOTICE])} streaming={null} />);
    // The verdict sentence, and NOT the window under it: a rule across the
    // column holds one line, and the whole wake-up is sixty journal lines long.
    expect(screen.getByText(VERDICT)).toBeInTheDocument();
    expect(screen.queryByText(new RegExp(WINDOW_LINE))).toBeNull();
    // The rest stays reachable rather than lost.
    expect(screen.getByText(VERDICT)).toHaveAttribute("title", NOTICE.text as string);
    // A clock time, not "3h ago": the point of the line is that this happened
    // while nobody was looking. The locale is the environment's, so the shape is
    // what is asserted, not the hour.
    expect(screen.getByText(/\d{1,2}[:.]\d{2}/)).toBeInTheDocument();
    // Not a bubble: no avatar row, which is what every spoken turn has.
    expect(screen.queryByText("A")).toBeNull();
  });

  it("renders the head's heartbeat reply as an ordinary bubble with a mark", () => {
    render(<AssistantConversation items={asItems([NOTICE, REPLY])} streaming={null} />);
    expect(screen.getByText(REPLY.text as string)).toBeInTheDocument();
    expect(screen.getByLabelText("Said by the heartbeat")).toBeInTheDocument();
  });

  it("leaves an ordinary turn unmarked", () => {
    render(
      <AssistantConversation
        items={asItems([{ id: "m3", role: "assistant", text: "hello" }])}
        streaming={null}
      />,
    );
    expect(screen.queryByLabelText("Said by the heartbeat")).toBeNull();
  });

  it("still draws the divider for a note whose text a peer did not send", () => {
    render(
      <AssistantConversation
        items={asItems([{ id: "m4", role: "system", kind: "heartbeat" }])}
        streaming={null}
      />,
    );
    expect(screen.getByText(/heartbeat woke the assistant/i)).toBeInTheDocument();
  });

  // A finished session's summary is its agent's whole closing message. The
  // strip printed it whole; the timeline prints one line and opens on a press.
  it("renders a journal row as one line that opens in place", () => {
    const closing = "The loader was already failing on main. I fixed the retry and the tests pass.";
    render(
      <AssistantConversation
        items={asItems(
          [{ id: "m5", role: "assistant", text: "on it", createdAt: "2026-09-12T08:00:00Z" }],
          [
            {
              id: 1,
              kind: "session_finished",
              sessionId: "s1",
              summary: closing,
              untrusted: true,
              at: "2026-09-12T08:05:00Z",
            },
          ],
        )}
        streaming={null}
      />,
    );
    const row = screen.getByRole("button", { expanded: false });
    expect(row).toHaveTextContent("Fix the loader");
    expect(screen.queryByRole("blockquote")).toBeNull();
    fireEvent.click(row);
    // Opened, an agent's words are a quotation with a caption, never plain text.
    expect(screen.getByText(closing).tagName).toBe("BLOCKQUOTE");
    expect(screen.getByText("finished, quoted")).toBeInTheDocument();
  });
});
