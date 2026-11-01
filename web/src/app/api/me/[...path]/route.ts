import { NextResponse, type NextRequest } from "next/server";
import { gateway } from "@/lib/gateway";
import { getToken } from "@/lib/session";

const SEGMENT = /^[A-Za-z0-9_-]+$/;
const KEY = /^[A-Za-z0-9_.:-]{8,255}$/;

// Server Actions check this automatically; route handlers don't.
function sameOrigin(request: NextRequest) {
  const origin = request.headers.get("origin");
  return origin !== null && new URL(origin).host === request.headers.get("host");
}

async function forward(request: NextRequest, { params }: RouteContext<"/api/me/[...path]">) {
  if (request.method !== "GET" && !sameOrigin(request)) {
    return NextResponse.json({ error: "cross-origin request rejected" }, { status: 403 });
  }
  const token = await getToken();
  if (!token) return NextResponse.json({ error: "session required" }, { status: 401 });

  const { path } = await params;
  if (!path.every((segment) => SEGMENT.test(segment))) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  const key = request.headers.get("Idempotency-Key");
  const raw = await request.text();

  const res = await gateway(request.method, `/me/${path.join("/")}`, {
    token,
    body: raw ? JSON.parse(raw) : undefined,
    idempotencyKey: key && KEY.test(key) ? key : undefined,
  });
  if (res.status === 204) return new NextResponse(null, { status: 204 });
  return NextResponse.json(res.data, {
    status: res.status,
    headers: res.replayed ? { "Idempotent-Replayed": "true" } : undefined,
  });
}

export { forward as GET, forward as POST, forward as PATCH, forward as DELETE };
