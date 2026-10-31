// The honest client (the "victim"). This is the shape the Next.js route
// handler will copy later: sign server-side, keep one key per logical action,
// and retry 409s with the *same* key.
import axios from "axios";
import { randomUUID } from "node:crypto";
import { sign } from "./signing.js";

/**
 * Request config with the retry count the response interceptor keeps on it.
 *
 * @typedef {import("axios").InternalAxiosRequestConfig & { __retries?: number }} RetryConfig
 */

/**
 * Creates the signing client. Every non-GET request is serialized, given an
 * `Idempotency-Key` (a new UUID unless the caller set one), timestamped and
 * signed. A `409` is retried with the same key.
 *
 * @param {object} options
 * @param {string} options.baseURL Target origin, with no path prefix.
 * @param {string} options.secret Shared secret used to sign requests.
 * @param {number} [options.maxRetries=5] How many `409` responses to retry.
 * @param {(config: import("axios").InternalAxiosRequestConfig) => import("axios").InternalAxiosRequestConfig} [options.tap]
 *   Interceptor that sees each request after signing. The demo's man in the
 *   middle is installed here.
 * @returns {import("axios").AxiosInstance} The configured client. It never
 *   rejects on HTTP status, so callers must check `res.status`.
 */
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

    const key = /** @type {string} */ (config.headers.get("Idempotency-Key") ?? randomUUID());
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
    const cfg = /** @type {RetryConfig} */ (res.config);
    cfg.__retries = cfg.__retries ?? 0;
    if (res.status !== 409 || cfg.__retries >= maxRetries) return res;
    cfg.__retries++;
    const wait = Number(res.headers["retry-after"] ?? 1) * 1000 * 0.25 * cfg.__retries;
    await new Promise((r) => setTimeout(r, wait));
    return client.request(cfg);
  });

  return client;
}
