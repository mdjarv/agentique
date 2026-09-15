/**
 * A session image loads through this page's server for every session, and a
 * remote session's image that fails there gets one direct try.
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }));

vi.mock("~/lib/machines/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("~/lib/machines/api")>();
  return {
    ...actual,
    apiFetch,
    sessionFileMachineId: (href: string) => (href.includes(REMOTE) ? "zbook" : undefined),
  };
});

import { useSessionImageSrc } from "~/hooks/useSessionImageSrc";

const LOCAL = "11111111-2222-4333-8444-555555555555";
const REMOTE = "99999999-2222-4333-8444-555555555555";

beforeEach(() => {
  apiFetch.mockReset();
  globalThis.URL.createObjectURL = vi.fn(() => "blob:direct");
  globalThis.URL.revokeObjectURL = vi.fn();
});

describe("useSessionImageSrc", () => {
  it("passes a data URL through", () => {
    const { result } = renderHook(() => useSessionImageSrc("data:image/png;base64,AA=="));
    expect(result.current).toEqual({ src: "data:image/png;base64,AA==" });
  });

  it("loads a remote session's image relative to this page, not from the machine", () => {
    const { result } = renderHook(() =>
      useSessionImageSrc(`/api/sessions/${REMOTE}/files/shot.png`),
    );
    expect(result.current.src).toBe(`/api/sessions/${REMOTE}/files/shot.png`);
    expect(apiFetch).not.toHaveBeenCalled();
  });

  it("normalises an absolute-localhost spelling to the relative path", () => {
    const { result } = renderHook(() =>
      useSessionImageSrc(`http://localhost:9201/api/sessions/${LOCAL}/files/shot.png`),
    );
    expect(result.current.src).toBe(`/api/sessions/${LOCAL}/files/shot.png`);
    expect(result.current.onError).toBeUndefined();
  });

  it("falls back to the machine directly once the relay fails", async () => {
    apiFetch.mockResolvedValue({ ok: true, blob: () => Promise.resolve(new Blob(["x"])) });
    const path = `/api/sessions/${REMOTE}/events/3/images/0`;
    const { result } = renderHook(() => useSessionImageSrc(path));

    act(() => result.current.onError?.());
    expect(result.current.src).toBeNull();
    await waitFor(() => expect(result.current.src).toBe("blob:direct"));
    expect(apiFetch).toHaveBeenCalledWith("zbook", path);
  });

  it("starts a new source on the relay again", () => {
    apiFetch.mockReturnValue(new Promise(() => {}));
    const { result, rerender } = renderHook(({ src }) => useSessionImageSrc(src), {
      initialProps: { src: `/api/sessions/${REMOTE}/files/a.png` },
    });
    act(() => result.current.onError?.());
    rerender({ src: `/api/sessions/${REMOTE}/files/b.png` });
    expect(result.current.src).toBe(`/api/sessions/${REMOTE}/files/b.png`);
  });
});
