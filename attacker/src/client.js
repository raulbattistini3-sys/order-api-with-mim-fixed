// The honest client (the "victim"). This is the shape the Next.js route
// handler will copy later: sign server-side, keep one key per logical action,
// and retry 409s with the *same* key.
import axios from "axios";
import { randomUUID } from "node:crypto";
import { sign } from "./signing.js";

export function createClient({ baseURL, secret, maxRetries = 5, tap }) {
  const client = axios.create({
    baseURL,
    timeout: 10_000,
    validateStatus: () => true,
    // Send exactly the bytes we signed; don't let axios re-serialize.
    transformRequest: [(data) => data],
  });

  // axios runs request interceptors last-registered first, so registering the
  // tap here makes it see the finished, signed request.
  if (tap) client.interceptors.request.use(tap);

  // Legit interceptor: serialize, attach key, timestamp and signature.
  client.interceptors.request.use((config) => {
    const method = (config.method ?? "get").toUpperCase();
    if (method === "GET") return config;

    if (typeof config.data !== "string") config.data = JSON.stringify(config.data ?? {});
    config.headers.set("Content-Type", "application/json");

    const key = config.headers.get("Idempotency-Key") ?? randomUUID();
    const ts = String(Date.now());
    const { pathname, search } = new URL(config.url, "http://x"); // baseURL must have no path prefix
    const uri = pathname + search;
    config.headers.set("Idempotency-Key", key);
    config.headers.set("X-Timestamp", ts);
    config.headers.set("X-Signature", sign(secret, method, uri, ts, key, config.data));
    return config;
  });

  // 409 = "same key still in flight". Retry with the same key (the request
  // interceptor reuses it and re-signs with a fresh timestamp).
  client.interceptors.response.use(async (res) => {
    const cfg = res.config;
    cfg.__retries = cfg.__retries ?? 0;
    if (res.status !== 409 || cfg.__retries >= maxRetries) return res;
    cfg.__retries++;
    const wait = Number(res.headers["retry-after"] ?? 1) * 1000 * 0.25 * cfg.__retries;
    await new Promise((r) => setTimeout(r, wait));
    return client.request(cfg);
  });

  return client;
}
