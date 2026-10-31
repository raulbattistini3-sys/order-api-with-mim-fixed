import { test } from "node:test";
import assert from "node:assert/strict";
import { sign } from "./signing.js";

// Same fixture as TestCanonicalMatchesJS in internal/signing/signing_test.go.
test("signature matches the Go implementation", () => {
  const got = sign("dev-secret", "POST", "/orders", "1700000000000", "key-00000001", '{"a":1}');
  assert.equal(got, "446e517c4c2a898a4cd71817b8392a7ba280813e1e39e8ba4559426ce12a7de7");
});
