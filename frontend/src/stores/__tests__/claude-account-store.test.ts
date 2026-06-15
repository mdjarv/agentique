import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  claudeLogin,
  claudeLoginCancel,
  claudeLoginCode,
  getClaudeAccount,
} from "~/lib/claude-account-api";
import { useClaudeAccountStore } from "~/stores/claude-account-store";

vi.mock("~/lib/claude-account-api", () => ({
  claudeLogin: vi.fn(),
  claudeLoginCancel: vi.fn(),
  claudeLoginCode: vi.fn(),
  claudeLogout: vi.fn(),
  getClaudeAccount: vi.fn(),
}));

/** Flush pending microtasks so awaited service steps reach their next setTimeout. */
async function flush() {
  for (let i = 0; i < 8; i++) await Promise.resolve();
}

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

  it("fetchStatus populates identity and clears loading", async () => {
    vi.mocked(getClaudeAccount).mockResolvedValue({
      loggedIn: true,
      email: "a@b.com",
      orgName: "Acme",
      subscriptionType: "max",
    });

    await useClaudeAccountStore.getState().fetchStatus();

    expect(useClaudeAccountStore.getState()).toMatchObject({
      loggedIn: true,
      email: "a@b.com",
      orgName: "Acme",
      subscriptionType: "max",
      loading: false,
    });
  });

  it("fetchStatus clears loading even when the request fails", async () => {
    vi.mocked(getClaudeAccount).mockRejectedValue(new Error("offline"));

    await useClaudeAccountStore.getState().fetchStatus();

    expect(useClaudeAccountStore.getState()).toMatchObject({
      loading: false,
      loggedIn: false,
    });
  });

  it("submitCode surfaces an error and does not start polling on failure", async () => {
    vi.mocked(claudeLoginCode).mockRejectedValue(new Error("bad code"));

    await useClaudeAccountStore.getState().submitCode("nope");

    expect(useClaudeAccountStore.getState()).toMatchObject({
      submittingCode: false,
      switching: false,
      error: "bad code",
    });
    expect(getClaudeAccount).not.toHaveBeenCalled();
  });

  it("cancelLogin aborts the poll and resets transient flow state", async () => {
    vi.mocked(claudeLogin).mockResolvedValue({ status: "login_started" });
    vi.mocked(getClaudeAccount).mockResolvedValue({ loggedIn: false });

    const login = useClaudeAccountStore.getState().loginAccount();
    await flush();
    expect(useClaudeAccountStore.getState().switching).toBe(true);

    await useClaudeAccountStore.getState().cancelLogin();
    expect(useClaudeAccountStore.getState()).toMatchObject({
      switching: false,
      loginUrl: null,
      loginDialogOpen: false,
      error: null,
    });

    // Advance past the poll interval: the aborted loop must not query the account.
    vi.mocked(getClaudeAccount).mockClear();
    await vi.advanceTimersByTimeAsync(2000);
    expect(getClaudeAccount).not.toHaveBeenCalled();
    await login;
  });

  it("a second flow supersedes a live poll instead of running two concurrently", async () => {
    // Regression for the old module-level pollAbortController stomp/leak: starting
    // a new flow must abort the previous flow's poll, not leave both looping.
    vi.mocked(claudeLogin).mockResolvedValue({ status: "login_started" });
    vi.mocked(claudeLoginCode).mockResolvedValue({ status: "ok" });
    vi.mocked(getClaudeAccount).mockResolvedValue({ loggedIn: false });

    const first = useClaudeAccountStore.getState().submitCode("code");
    await flush();
    const second = useClaudeAccountStore.getState().loginAccount();
    await flush();

    vi.mocked(getClaudeAccount).mockClear();
    await vi.advanceTimersByTimeAsync(2000);

    // Only the surviving (second) poll should query the account this interval.
    expect(getClaudeAccount).toHaveBeenCalledTimes(1);

    // Cancel, then advance so both flows' pending poll callbacks fire, observe
    // their aborted signal, and resolve their promises.
    await useClaudeAccountStore.getState().cancelLogin();
    await vi.advanceTimersByTimeAsync(2000);
    await first;
    await second;
  });
});
