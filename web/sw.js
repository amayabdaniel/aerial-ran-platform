// Minimal service worker — install + cache shell + network-first for /api.
const CACHE = 'aerial-v1';
const SHELL = ['/ui/', '/ui/index.html', '/ui/manifest.webmanifest', '/ui/icon-192.png', '/ui/icon-512.png'];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (url.pathname.startsWith('/api/')) {
    // Network-first for API calls.
    e.respondWith(fetch(e.request).catch(() => new Response(JSON.stringify({offline: true}), {status: 503})));
    return;
  }
  // Cache-first for UI shell.
  e.respondWith(caches.match(e.request).then((r) => r || fetch(e.request)));
});
