/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  env: {
    NEXT_PUBLIC_BUILD_VERSION: process.env.NEXT_PUBLIC_BUILD_VERSION || 'dev',
  },
  images: {
    remotePatterns: [
      {
        protocol: 'https',
        hostname: '**',
      },
    ],
  },
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: `${process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}/api/:path*`,
      },
    ]
  },
  async headers() {
    const noStore = [
      {
        key: 'Cache-Control',
        value: 'no-store, no-cache, must-revalidate, proxy-revalidate',
      },
    ]
    return [
      { source: '/', headers: noStore },
      { source: '/login', headers: noStore },
      { source: '/signup', headers: noStore },
      { source: '/dashboard/:path*', headers: noStore },
      { source: '/d/:path*', headers: noStore },
      { source: '/f/:path*', headers: noStore },
    ]
  },
}

module.exports = nextConfig
