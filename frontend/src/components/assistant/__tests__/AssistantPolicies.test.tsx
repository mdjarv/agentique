/**
 * The policies page: the rows the server holds, and one blank row.
 *
 * The assertions worth having here are the two that would lose somebody's rule:
 * a Save sends what the row now says (id included, so an edit is an edit and not
 * a second policy), and the page then renders **what came back** rather than
 * what it sent — the server clamps the budgets and caps the text, and a page
 * holding its own copy would show a row nobody could act on.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantPoliciesPage } from "~/components/assistant/AssistantPolicies";
import type { AssistantPolicy } from "~/lib/assistant/wire";
import { useAssistantStore } from "~/stores/assistant-store";
import { useFeatureStore } from "~/stores/feature-store";

const savePolicy = vi.fn();
const deletePolicy = vi.fn();

vi.mock("~/hooks/useWebSocket", () => ({ useWebSocket: () => ({}) }));
vi.mock("~/lib/assistant/rpc", () => ({
  savePolicy: (...args: unknown[]) => savePolicy(...args),
  deletePolicy: (...args: unknown[]) => deletePolicy(...args),
}));
// The header is not the subject and brings a call view and a router with it.
vi.mock("~/components/assistant/AssistantHeader", () => ({
  AssistantHeader: ({ title }: { title: string }) => <h1>{title}</h1>,
}));

const POLICY: AssistantPolicy = {
  id: "pol-1",
  name: "keep the tests green",
  text: "When a session's tests go red, start one to fix them.",
  enabled: true,
  budgetInFlight: 1,
  budgetPerDay: 3,
};

beforeEach(() => {
  useAssistantStore.getState().reset();
  useFeatureStore.setState({
    features: { browser: false, teams: false, assistant: true, brain: false, voice: false },
    loaded: true,
  });
  savePolicy.mockReset();
  deletePolicy.mockReset();
  savePolicy.mockResolvedValue(POLICY);
  deletePolicy.mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("AssistantPoliciesPage", () => {
  it("renders a row per policy plus a blank one, every control with a stable id", () => {
    useAssistantStore.getState().setPolicies([POLICY]);
    render(<AssistantPoliciesPage />);

    expect(screen.getByLabelText("Name", { selector: "#policy-name-pol-1" })).toHaveValue(
      "keep the tests green",
    );
    expect(screen.getByLabelText("Instruction", { selector: "#policy-text-pol-1" })).toHaveValue(
      POLICY.text,
    );
    expect(screen.getByLabelText("At once", { selector: "#policy-in-flight-pol-1" })).toHaveValue(
      1,
    );
    expect(screen.getByLabelText("Per day", { selector: "#policy-per-day-pol-1" })).toHaveValue(3);
    expect(screen.getByLabelText("Enabled", { selector: "#policy-enabled-pol-1" })).toBeChecked();
    // The blank row is the same five controls, keyed `new`.
    expect(screen.getByLabelText("Name", { selector: "#policy-name-new" })).toHaveValue("");
  });

  it("sends the edited row and renders what came back", async () => {
    useAssistantStore.getState().setPolicies([POLICY]);
    savePolicy.mockResolvedValue({ ...POLICY, name: "clamped", budgetPerDay: 1 });
    render(<AssistantPoliciesPage />);

    fireEvent.change(screen.getByLabelText("Name", { selector: "#policy-name-pol-1" }), {
      target: { value: "green tests" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(savePolicy).toHaveBeenCalledTimes(1));
    expect(savePolicy.mock.calls[0]?.[1]).toEqual({
      id: "pol-1",
      name: "green tests",
      text: POLICY.text,
      enabled: true,
      budgetInFlight: 1,
      budgetPerDay: 3,
    });
    // The server's answer wins over the draft, clamp included.
    await waitFor(() =>
      expect(screen.getByLabelText("Name", { selector: "#policy-name-pol-1" })).toHaveValue(
        "clamped",
      ),
    );
    expect(screen.getByLabelText("Per day", { selector: "#policy-per-day-pol-1" })).toHaveValue(1);
  });

  it("saves the blank row with no id, so the server mints one", async () => {
    render(<AssistantPoliciesPage />);
    fireEvent.change(screen.getByLabelText("Name", { selector: "#policy-name-new" }), {
      target: { value: "watch the loops" },
    });
    fireEvent.click(screen.getByRole("button", { name: /add/i }));

    await waitFor(() => expect(savePolicy).toHaveBeenCalledTimes(1));
    expect(savePolicy.mock.calls[0]?.[1]).not.toHaveProperty("id");
    // The saved row lands in the store, and the blank row is blank again.
    await waitFor(() => expect(useAssistantStore.getState().policies).toHaveLength(1));
    expect(screen.getByLabelText("Name", { selector: "#policy-name-new" })).toHaveValue("");
  });

  it("refuses to save a nameless policy, because the head could not name it", () => {
    render(<AssistantPoliciesPage />);
    expect(screen.getByRole("button", { name: /add/i })).toBeDisabled();
  });

  it("deletes a row and drops it locally", async () => {
    useAssistantStore.getState().setPolicies([POLICY]);
    render(<AssistantPoliciesPage />);
    fireEvent.click(screen.getByRole("button", { name: /delete/i }));
    await waitFor(() => expect(deletePolicy).toHaveBeenCalledWith({}, "pol-1"));
    await waitFor(() => expect(useAssistantStore.getState().policies).toHaveLength(0));
  });

  it("says so when the assistant is off, rather than drawing rules nothing reads", () => {
    useFeatureStore.setState({
      features: { browser: false, teams: false, assistant: false, brain: false, voice: false },
      loaded: true,
    });
    render(<AssistantPoliciesPage />);
    expect(screen.getByText(/assistant is off/i)).toBeTruthy();
    expect(screen.queryByLabelText("Name", { selector: "#policy-name-new" })).toBeNull();
  });
});
