/* sw.js — le service worker des notifications d'appel. Il n'intercepte aucune
   requête (pas de gestionnaire fetch) : il ne fait qu'afficher les push et
   ouvrir le salon au clic. Le contenu arrive chiffré (push.go). */

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

self.addEventListener('push', (e) => {
  let d = {};
  try { d = e.data ? e.data.json() : {}; } catch { /* contenu illisible : notification générique */ }
  const url = typeof d.url === 'string' && d.url.startsWith('/') ? d.url : '/';
  e.waitUntil(self.registration.showNotification(d.title || 'Sloubi.chat', {
    body: d.body || 'Quelqu’un t’appelle.',
    tag: d.tag || 'sloubi-call',   // un nouvel appel du même salon remplace l'ancien
    renotify: true,
    icon: '/icon-192.png', // sur un domaine perso : l'initiale du lieu (icons.go)
    badge: '/icon-192.png',
    data: { url },
  }));
});

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = new URL(e.notification.data?.url || '/', self.location.origin).href;
  e.waitUntil((async () => {
    const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const open = wins.find((w) => w.url.split('#')[0] === url);
    if (open) return open.focus();
    return self.clients.openWindow(url);
  })());
});
