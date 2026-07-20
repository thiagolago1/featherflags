import { auth } from "@/auth";

// The only place in this app that knows the Go API's address and service
// token. The browser only ever talks to /api/backend/*, same-origin, with
// its httpOnly session cookie — INTERNAL_API_URL and INTERNAL_API_TOKEN
// never reach client-side JS.
const INTERNAL_API_URL = process.env.INTERNAL_API_URL;
const INTERNAL_API_TOKEN = process.env.INTERNAL_API_TOKEN;

// Only the admin surface is proxied. /v1/evaluate and /v1/stream are for
// SDK consumers authenticated by per-project API key, not for the dashboard.
const ALLOWED_PREFIX = "admin/";

async function proxy(req: Request, path: string[]): Promise<Response> {
  const session = await auth();
  if (!session?.user) {
    return Response.json({ error: "unauthorized" }, { status: 401 });
  }
  if (!INTERNAL_API_URL || !INTERNAL_API_TOKEN) {
    return Response.json({ error: "backend not configured" }, { status: 500 });
  }

  const joined = path.join("/");
  if (!joined.startsWith(ALLOWED_PREFIX)) {
    return Response.json({ error: "not found" }, { status: 404 });
  }

  const url = new URL(`/${joined}`, INTERNAL_API_URL);
  url.search = new URL(req.url).search;

  const upstream = await fetch(url, {
    method: req.method,
    headers: {
      Authorization: `Bearer ${INTERNAL_API_TOKEN}`,
      "Content-Type": "application/json",
    },
    body: ["GET", "HEAD"].includes(req.method) ? undefined : await req.text(),
    // Server-to-server call inside the cluster network; no browser caching.
    cache: "no-store",
  });

  const body = await upstream.text();
  return new Response(body, {
    status: upstream.status,
    headers: { "Content-Type": upstream.headers.get("Content-Type") ?? "application/json" },
  });
}

export async function GET(req: Request, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(req, (await params).path);
}
export async function POST(req: Request, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(req, (await params).path);
}
export async function PATCH(req: Request, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(req, (await params).path);
}
