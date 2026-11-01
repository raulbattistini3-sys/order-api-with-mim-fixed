// Browser side. Every call goes to this app's /api/me routes, which add the
// session and signature. The browser never sees either.

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export type ApiResult<T> = { data: T; replayed: boolean };

export async function api<T>(path: string, init?: RequestInit): Promise<ApiResult<T>> {
  const res = await fetch(`/api/me/${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (res.status === 401) {
    // A full reload, so the next user can't see this user's cached queries.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.assign(`/login?next=${encodeURIComponent(window.location.pathname)}`);
    throw new ApiError(401, "Sessão expirada.");
  }
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) throw new ApiError(res.status, data?.error ?? "Algo deu errado. Tente novamente.");
  return { data, replayed: res.headers.get("Idempotent-Replayed") === "true" };
}
