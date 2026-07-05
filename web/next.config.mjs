/** @type {import('next').NextConfig} */
const nextConfig = {
  // Proxy /api/* to the Go API so the browser and API share an origin: the
  // session cookie is first-party and no CORS is required. Override the target
  // with API_BASE_URL in other environments.
  async rewrites() {
    const apiBase = process.env.API_BASE_URL || "http://localhost:8080";
    return [{ source: "/api/:path*", destination: `${apiBase}/:path*` }];
  },
};

export default nextConfig;
