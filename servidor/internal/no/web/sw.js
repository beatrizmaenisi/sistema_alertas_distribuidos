const CACHE = 'alertas-v1';
const MAPA = 'tiles.openfreemap.org';
const PRAZO_MS = 3000;
const CASCA = [
  '/', '/manifest.json', '/icones/icone.svg', '/icones/icone-192.png',
  '/css/painel.css', '/css/maplibre-gl.min.css',
  '/js/painel.js', '/js/fila.js', '/js/htmx.min.js', '/js/h3-js.umd.js', '/js/deck.gl.min.js', '/js/maplibre-gl.min.js',
];

self.addEventListener('install', e => e.waitUntil(
  caches.open(CACHE).then(c => c.addAll(CASCA)).then(() => self.skipWaiting())));

self.addEventListener('activate', e => e.waitUntil(
  caches.keys()
    .then(nomes => Promise.all(nomes.filter(n => n !== CACHE).map(n => caches.delete(n))))
    .then(() => self.clients.claim())));

const comPrazo = promessa => Promise.race([
  promessa, new Promise((_, falha) => setTimeout(() => falha(new Error('prazo')), PRAZO_MS)),
]);

async function redePrimeiro(pedido) {
  const cache = await caches.open(CACHE);
  try {
    const resposta = await comPrazo(fetch(pedido));
    if (resposta.ok) cache.put(pedido, resposta.clone());
    return resposta;
  } catch {
    return await cache.match(pedido) || Response.error();
  }
}

// As consultas ao estado (/painel, /api) ficam de fora: offline, devem falhar
// para a página avisar que está sem conexão em vez de mostrar dados velhos como novos.
self.addEventListener('fetch', e => {
  const url = new URL(e.request.url);
  const casca = url.origin === location.origin && (url.pathname === '/' || /^\/(js|css|icones)\/|^\/manifest\.json$/.test(url.pathname));
  if (e.request.method === 'GET' && (casca || url.hostname === MAPA)) e.respondWith(redePrimeiro(e.request));
});
