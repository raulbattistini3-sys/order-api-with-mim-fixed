// Mirror of internal/signing/signing.go. Keep the canonical string identical.
import { createHash, createHmac } from "node:crypto";

export function canonical(method, requestURI, timestamp, key, body) {
  const bodyHash = createHash("sha256").update(body ?? "").digest("hex");
  return [method.toUpperCase(), requestURI, timestamp, key, bodyHash].join("\n");
}

export function sign(secret, method, requestURI, timestamp, key, body) {
  return createHmac("sha256", secret).update(canonical(method, requestURI, timestamp, key, body)).digest("hex");
}
