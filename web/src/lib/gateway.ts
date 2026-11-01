import "server-only";

import { createHash, createHmac, randomUUID } from "node:crypto";

export type GatewayResponse<T = unknown> = {
  status: number;
  data: T;
  replayed: boolean;
};

type Options = {
  body?: unknown;
  token?: string;
  idempotencyKey?: string;
};

type RawOptions = {
  body: Uint8Array;
  contentType: string;
  token?: string;
  idempotencyKey?: string;
};

// Same canonical string as internal/signing/signing.go. bodyHash is hex
// sha256 of the exact bytes sent.
function signCanonical(method: string, uri: string, timestamp: string, key: string, bodyHash: string) {
  return createHmac("sha256", env("CLIENT_SECRET"))
    .update([method, uri, timestamp, key, bodyHash].join("\n"))
    .digest("hex");
}

/** Signs a request whose body is a string (kept for the shared signature test). */
export function sign(secret: string, method: string, uri: string, timestamp: string, key: string, body: string) {
  const bodyHash = createHash("sha256").update(body).digest("hex");
  return createHmac("sha256", secret)
    .update([method, uri, timestamp, key, bodyHash].join("\n"))
    .digest("hex");
}

function env(name: string) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set; copy web/.env.example to web/.env.local`);
  return value;
}

async function send<T>(
  method: string,
  uri: string,
  body: Uint8Array,
  contentType: string,
  token: string | undefined,
  idempotencyKey: string,
): Promise<GatewayResponse<T>> {
  const timestamp = String(Date.now());
  const bodyHash = createHash("sha256").update(body).digest("hex");
  const headers: Record<string, string> = {
    "Content-Type": contentType,
    "Idempotency-Key": idempotencyKey,
    "X-Timestamp": timestamp,
    "X-Signature": signCanonical(method, uri, timestamp, idempotencyKey, bodyHash),
  };
  if (token) headers.Authorization = `Bearer ${token}`;

  try {
    const res = await fetch(env("GATEWAY_URL") + uri, {
      method,
      headers,
      // undici accepts a Uint8Array body at runtime; its types list only BodyInit.
      body: body.length ? (body as BodyInit) : undefined,
      cache: "no-store",
    });
    const text = await res.text();
    return {
      status: res.status,
      data: (text ? JSON.parse(text) : null) as T,
      replayed: res.headers.get("Idempotent-Replayed") === "true",
    };
  } catch (err) {
    console.error("gateway request failed", err);
    return { status: 503, data: null as T, replayed: false };
  }
}

/**
 * Sends a signed JSON request to the gateway. A network failure comes back as
 * status 503, so callers handle every failure by checking the status.
 */
export function gateway<T = unknown>(
  method: string,
  uri: string,
  { body, token, idempotencyKey = randomUUID() }: Options = {},
): Promise<GatewayResponse<T>> {
  const raw = Buffer.from(body === undefined ? "" : JSON.stringify(body));
  return send<T>(method, uri, raw, "application/json", token, idempotencyKey);
}

/** Sends a signed request with a raw binary body, such as an uploaded file. */
export function gatewayRaw<T = unknown>(
  method: string,
  uri: string,
  { body, contentType, token, idempotencyKey = randomUUID() }: RawOptions,
): Promise<GatewayResponse<T>> {
  return send<T>(method, uri, body, contentType, token, idempotencyKey);
}
