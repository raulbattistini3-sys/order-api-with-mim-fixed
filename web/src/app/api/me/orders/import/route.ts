import { NextResponse, type NextRequest } from "next/server";
import { gatewayRaw } from "@/lib/gateway";
import { getToken } from "@/lib/session";

const MAX_BYTES = 64 << 20;

// The file bytes are signed and forwarded as-is; the API parses CSV or XLSX by
// sniffing the content, so the content type is only a hint.
export async function POST(request: NextRequest) {
  const origin = request.headers.get("origin");
  if (origin === null || new URL(origin).host !== request.headers.get("host")) {
    return NextResponse.json({ error: "cross-origin request rejected" }, { status: 403 });
  }
  const token = await getToken();
  if (!token) return NextResponse.json({ error: "session required" }, { status: 401 });

  const body = new Uint8Array(await request.arrayBuffer());
  if (body.byteLength > MAX_BYTES) {
    return NextResponse.json({ error: "file too large" }, { status: 413 });
  }

  const res = await gatewayRaw("POST", "/me/orders/import", {
    token,
    body,
    contentType: request.headers.get("content-type") ?? "application/octet-stream",
  });
  return NextResponse.json(res.data, { status: res.status });
}
