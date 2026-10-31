// Mirror of internal/signing/signing.go. Keep the canonical string identical.
import { createHash, createHmac } from "node:crypto";

/**
 * Builds the exact string that gets signed.
 *
 * @param {string} method HTTP method; uppercased before signing.
 * @param {string} requestURI Path and query, such as `/orders?a=1`.
 * @param {string} timestamp Unix time in milliseconds.
 * @param {string} key The `Idempotency-Key` value.
 * @param {string} [body] Raw request body; a missing body hashes as empty.
 * @returns {string} The five fields joined by newlines.
 */
export function canonical(method, requestURI, timestamp, key, body) {
  const bodyHash = createHash("sha256").update(body ?? "").digest("hex");
  return [method.toUpperCase(), requestURI, timestamp, key, bodyHash].join("\n");
}

/**
 * Signs a request with HMAC-SHA256, for the `X-Signature` header.
 *
 * @param {string} secret Shared secret; must stay on the server side.
 * @param {string} method HTTP method.
 * @param {string} requestURI Path and query.
 * @param {string} timestamp Unix time in milliseconds.
 * @param {string} key The `Idempotency-Key` value.
 * @param {string} [body] Raw request body, exactly as sent.
 * @returns {string} The signature in lowercase hex.
 */
export function sign(secret, method, requestURI, timestamp, key, body) {
  return createHmac("sha256", secret).update(canonical(method, requestURI, timestamp, key, body)).digest("hex");
}
