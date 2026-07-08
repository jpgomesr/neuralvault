import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getMe, login, logout } from "./auth";
import { jsonResponse } from "./test-helpers";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

describe("getMe", () => {
  it("returns the user on 200", async () => {
    const me = { id: "u1", email: "a@b.com" };
    fetchMock.mockResolvedValueOnce(jsonResponse(me));
    await expect(getMe()).resolves.toEqual(me);
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/me");
  });

  it("returns null when unauthenticated (401)", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 401 }));
    await expect(getMe()).resolves.toBeNull();
  });

  it("throws on a non-401 error status", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(getMe()).rejects.toThrow("auth/me failed: 500");
  });
});

describe("logout", () => {
  it("POSTs to the logout endpoint", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null));
    await logout();
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/logout", { method: "POST" });
  });
});

describe("login", () => {
  it("POSTs credentials as JSON", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { status: 204 }));
    await login("a@b.com", "pw");
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/token", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "a@b.com", password: "pw" }),
    });
  });

  it("throws a specific message on 401", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 401 }));
    await expect(login("a@b.com", "wrong")).rejects.toThrow("invalid email or password");
  });

  it("throws on other error statuses", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(null, { ok: false, status: 500 }));
    await expect(login("a@b.com", "pw")).rejects.toThrow("login failed: 500");
  });
});
