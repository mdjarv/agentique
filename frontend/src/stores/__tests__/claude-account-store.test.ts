import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { claudeLogin, claudeLoginCancel, getClaudeAccount } from "~/lib/claude-account-api";
import { useClaudeAccountStore } from "~/stores/claude-account-store";

vi.mock("~/lib/claude-account-api", () => ({
  claudeLogin: vi.fn(),
  claudeLoginCancel: vi.fn(),
  claudeLoginCode: vi.fn(),
  claudeLogout: vi.fn(),
  getClaudeAccount: vi.fn(),
}));

describe("claude-account-store", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(claudeLoginCancel).mockResolvedValue(undefined);
    useClaudeAccountStore.setState({
      loggedIn: false,
      email: null,
      orgName: null,
      subscriptionType: null,
      loading: true,
      switching: false,
      loginUrl: null,
      loginDialogOpen: false,
      error: null,
      submittingCode: false,
    });
  });

  afterEach(() => {
    useClaudeAccountStore.getState().cancelLogin();
    vi.useRealTimers();
    vi.resetAllMocks();
  });

  it("keeps polling after a transient account status failure", async () => {
    vi.mocked(claudeLogin).mockResolvedValue({ status: "login_started" });
    vi.mocked(getClaudeAccount)
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce({
        loggedIn: true,
        email: "dev@example.com",
        orgName: "Example",
        subscriptionType: "pro",
      });

    const login = useClaudeAccountStore.getState().loginAccount();
    await vi.advanceTimersByTimeAsync(2000);
    await vi.advanceTimersByTimeAsync(2000);
    await login;

    expect(useClaudeAccountStore.getState()).toMatchObject({
      loggedIn: true,
      email: "dev@example.com",
      switching: false,
      loginDialogOpen: false,
      error: null,
    });
  });
});
