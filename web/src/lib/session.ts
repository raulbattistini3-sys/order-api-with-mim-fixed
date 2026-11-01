import "server-only";

import { cookies } from "next/headers";
import { cache } from "react";
import { gateway } from "./gateway";
import type { Me } from "./types";

const COOKIE = "session";
const MAX_AGE_SECONDS = 8 * 60 * 60; // users.authToken.duration in the migration

// httpOnly is what protects the token from XSS: page scripts can't read it.
export async function setSession(token: string) {
  (await cookies()).set(COOKIE, token, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: MAX_AGE_SECONDS,
  });
}

export async function clearSession() {
  (await cookies()).delete(COOKIE);
}

export async function getToken() {
  return (await cookies()).get(COOKIE)?.value;
}

/** Returns the logged-in user, or null. Cached for one render. */
export const getMe = cache(async (): Promise<Me | null> => {
  const token = await getToken();
  if (!token) return null;

  const res = await gateway<Me>("GET", "/me", { token });
  if (res.status === 401) return null;
  if (res.status !== 200) throw new Error(`GET /me: ${res.status}`);
  return res.data;
});
