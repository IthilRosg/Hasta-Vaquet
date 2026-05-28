// Cloudflare Worker for DNS-over-HTTPS proxy
// Deploy to workers.dev or custom domain behind Cloudflare
// DNS queries go through Cloudflare → target DoH server
// DPI sees: HTTPS to Cloudflare IP (not blocked)
// Target server sees: Cloudflare IP, not client IP

// --- Configuration ---
const UPSTREAM_DOH = 'https://1.1.1.1/dns-query'
const FALLBACK_DOH = 'https://8.8.8.8/dns-query'
const RATE_LIMIT_RPM = 200          // max requests per minute per IP
const RATE_LIMIT_WINDOW_MS = 60_000 // 1 minute sliding window

// --- Rate limiter (in-memory, per-worker) ---
const rateMap = new Map()

function isRateLimited(ip) {
  const now = Date.now()
  const windowStart = now - RATE_LIMIT_WINDOW_MS

  let entries = rateMap.get(ip)
  if (!entries) {
    entries = []
    rateMap.set(ip, entries)
  }

  // Drop entries outside the sliding window
  while (entries.length > 0 && entries[0] < windowStart) {
    entries.shift()
  }

  if (entries.length >= RATE_LIMIT_RPM) {
    return true
  }

  entries.push(now)
  return false
}

// --- Logging ---
const logReq = (ip, method, path, status) => {
  console.log(`[DOH] ${method} ${path} → ${status} | ${ip}`)
}

// --- DNS wireformat helpers ---
function base64urlToBytes(b64url) {
  // Restore padding
  let b64 = b64url.replace(/-/g, '+').replace(/_/g, '/')
  while (b64.length % 4) b64 += '='
  const binStr = atob(b64)
  const bytes = new Uint8Array(binStr.length)
  for (let i = 0; i < binStr.length; i++) {
    bytes[i] = binStr.charCodeAt(i)
  }
  return bytes
}

function bytesToBase64url(bytes) {
  let binStr = ''
  for (let i = 0; i < bytes.length; i++) {
    binStr += String.fromCharCode(bytes[i])
  }
  return btoa(binStr).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// --- DNS query forwarding ---
async function resolveDns(dnsBytes, upstream) {
  const resp = await fetch(upstream, {
    method: 'POST',
    headers: {
      'Accept': 'application/dns-message',
      'Content-Type': 'application/dns-message',
    },
    body: dnsBytes,
  })

  if (!resp.ok) {
    throw new Error(`Upstream returned ${resp.status}`)
  }

  return resp
}

// --- CORS headers ---
const corsHeaders = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type',
  'Access-Control-Max-Age': '86400',
}

// --- Main request handler ---
async function handleRequest(request) {
  const url = new URL(request.url)
  const clientIP = request.headers.get('CF-Connecting-IP') || 'unknown'

  // CORS preflight
  if (request.method === 'OPTIONS') {
    return new Response(null, {
      status: 204,
      headers: corsHeaders,
    })
  }

  // Rate limiting
  if (isRateLimited(clientIP)) {
    logReq(clientIP, request.method, url.pathname, 429)
    return new Response('Too Many Requests', {
      status: 429,
      headers: {
        ...corsHeaders,
        'Retry-After': '60',
      },
    })
  }

  let dnsBytes = null

  try {
    if (request.method === 'GET') {
      // GET /dns-query?dns=<base64url>
      const dnsParam = url.searchParams.get('dns')
      if (!dnsParam) {
        return new Response('Missing ?dns= parameter', {
          status: 400,
          headers: corsHeaders,
        })
      }
      dnsBytes = base64urlToBytes(dnsParam)
    } else if (request.method === 'POST') {
      // POST /dns-query with Content-Type: application/dns-message
      const contentType = request.headers.get('Content-Type') || ''
      if (contentType !== 'application/dns-message') {
        return new Response('Expected Content-Type: application/dns-message', {
          status: 400,
          headers: corsHeaders,
        })
      }
      dnsBytes = await request.arrayBuffer()
      dnsBytes = new Uint8Array(dnsBytes)
    } else {
      return new Response('Method Not Allowed', {
        status: 405,
        headers: corsHeaders,
      })
    }

    if (!dnsBytes || dnsBytes.length === 0) {
      return new Response('Empty DNS message', {
        status: 400,
        headers: corsHeaders,
      })
    }
  } catch (e) {
    logReq(clientIP, request.method, url.pathname, 400)
    return new Response(`Bad request: ${e.message}`, {
      status: 400,
      headers: corsHeaders,
    })
  }

  // Forward to upstream DoH
  let lastError = null
  for (const upstream of [UPSTREAM_DOH, FALLBACK_DOH]) {
    try {
      const resp = await resolveDns(dnsBytes, upstream)

      const respBytes = await resp.arrayBuffer()

      logReq(clientIP, request.method, url.pathname, resp.status)

      return new Response(respBytes, {
        status: resp.status,
        headers: {
          ...corsHeaders,
          'Content-Type': 'application/dns-message',
          'X-DoH-Upstream': upstream.replace(/^https?:\/\//, ''),
        },
      })
    } catch (e) {
      lastError = e
    }
  }

  // Both upstreams failed
  logReq(clientIP, request.method, url.pathname, 502)
  return new Response(`DNS resolution failed: ${lastError?.message || 'unknown'}`, {
    status: 502,
    headers: corsHeaders,
  })
}

// --- Entry point ---
export default {
  async fetch(request, env, ctx) {
    return handleRequest(request)
  },
}
