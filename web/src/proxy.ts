import { NextResponse, type NextRequest } from "next/server";

// A fast first check only: it sees that a cookie exists, not that it is valid.
// The real check is in (app)/layout.tsx and, for data, in the gateway.
export function proxy(request: NextRequest) {
  if (request.cookies.has("session")) return NextResponse.next();

  const login = new URL("/login", request.url);
  login.searchParams.set("next", request.nextUrl.pathname);
  return NextResponse.redirect(login);
}

export const config = {
  matcher: ["/dashboard/:path*", "/orders/:path*", "/todos/:path*"],
};
