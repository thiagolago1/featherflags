/** @type {import('next').NextConfig} */
const nextConfig = {
  // Standalone output for a minimal Docker runtime image (see Dockerfile).
  output: "standalone",
  // Server-side rendering only; the dashboard never needs to ship the
  // backend URL or any secret to the client bundle. Everything under
  // src/app/api/backend is the only thing allowed to talk to the API.
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "same-origin" },
        ],
      },
    ];
  },
};

export default nextConfig;
