import { useSyncExternalStore } from "react";
import { redirect } from "@tanstack/react-router";

import type { AuthIdentity, LoginResponse } from "@/types";

const authStorageKey = "tinycdn-auth";
let cachedSession: AuthSession | null = null;
let initialized = false;
let storageListenerBound = false;

export class UnauthorizedError extends Error {
  constructor(message = "Unauthorized") {
    super(message);
    this.name = "UnauthorizedError";
  }
}

export interface AuthSession {
  token: string;
  user: AuthIdentity;
  expiresAt: string;
}

const listeners = new Set<() => void>();

function notify() {
  for (const listener of listeners) {
    listener();
  }
}

function readStoredSession(): AuthSession | null {
  if (typeof window === "undefined") {
    return null;
  }
  const raw = window.localStorage.getItem(authStorageKey);
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as AuthSession;
    if (!parsed.token || !parsed.user?.username || !parsed.expiresAt) {
      window.localStorage.removeItem(authStorageKey);
      return null;
    }
    if (isExpired(parsed)) {
      window.localStorage.removeItem(authStorageKey);
      return null;
    }
    return parsed;
  } catch {
    window.localStorage.removeItem(authStorageKey);
    return null;
  }
}

function ensureAuthStore() {
  if (typeof window === "undefined") {
    return;
  }
  if (!initialized) {
    cachedSession = readStoredSession();
    initialized = true;
  }
  if (storageListenerBound) {
    return;
  }
  window.addEventListener("storage", (event) => {
    if (event.key !== authStorageKey) {
      return;
    }
    cachedSession = readStoredSession();
    notify();
  });
  storageListenerBound = true;
}

export function getAuthSession() {
  ensureAuthStore();
  return cachedSession;
}

export function getAuthToken() {
  return getAuthSession()?.token ?? null;
}

export function setAuthSession(payload: LoginResponse | AuthSession | null) {
  if (typeof window === "undefined") {
    return;
  }
  ensureAuthStore();
  if (!payload) {
    window.localStorage.removeItem(authStorageKey);
    cachedSession = null;
    notify();
    return;
  }
  const session: AuthSession =
    "expires_at" in payload
      ? {
          token: payload.token,
          user: payload.user,
          expiresAt: payload.expires_at,
        }
      : payload;
  window.localStorage.setItem(authStorageKey, JSON.stringify(session));
  cachedSession = session;
  notify();
}

export function clearAuthSession() {
  setAuthSession(null);
}

export function subscribeAuth(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useAuthSession() {
  return useSyncExternalStore(subscribeAuth, getAuthSession, getAuthSession);
}

export function isExpired(session: AuthSession) {
  const expiresAt = Date.parse(session.expiresAt);
  return Number.isNaN(expiresAt) || expiresAt <= Date.now();
}

function redirectTarget(location: { href?: string; pathname?: string } | undefined) {
  if (!location) {
    return "/";
  }
  return location.href || location.pathname || "/";
}

export function requireAuth(location?: { href?: string; pathname?: string }) {
  const session = getAuthSession();
  if (!session) {
    throw redirect({
      to: "/login",
      search: {
        redirect: redirectTarget(location),
      },
    });
  }
  return session;
}

export async function withProtectedLoader<T>(
  location: { href?: string; pathname?: string } | undefined,
  load: () => Promise<T>,
) {
  requireAuth(location);
  try {
    return await load();
  } catch (error) {
    if (error instanceof UnauthorizedError) {
      clearAuthSession();
      throw redirect({
        to: "/login",
        search: {
          redirect: redirectTarget(location),
        },
      });
    }
    throw error;
  }
}
