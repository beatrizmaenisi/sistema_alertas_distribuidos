import * as fila from './fila.js';

const cores = { INFO: '#3b82f6', ATENCAO: '#d97706', EMERGENCIA: '#dc2626' };
const $ = id => document.getElementById(id);
const novoAutor = () => 'painel-' + Math.random().toString(36).slice(2);
const nomesNivel = { INFO: 'Informativo', ATENCAO: 'Atenção', EMERGENCIA: 'Emergência' };

// O aparelho é sempre a mesma pessoa, mesmo depois de fechar e reabrir o aplicativo.
function autorDoAparelho() {
  try { return localStorage.autor ??= novoAutor(); } catch { return novoAutor(); }
}
const autorFixo = autorDoAparelho();
const estreita = matchMedia('(max-width: 900px)');
// Em telas estreitas os widgets começam recolhidos, para o mapa aparecer.
if (estreita.matches) for (const d of document.querySelectorAll('details.widget')) d.open = false;

let mapa = null, mapaPronto = false;
// Áreas desenhadas agora, por chave H3: o HTML do popup e o centro do hexágono.
let areasAtuais = {};
if (window.maplibregl) {
  // O zoom do MapLibre (blocos de 512px) fica um nível abaixo do Leaflet para a mesma escala.
  mapa = new maplibregl.Map({
    container: 'mapa', style: 'https://tiles.openfreemap.org/styles/positron',
    center: [-46.63, -23.55], zoom: 11, maxZoom: 18, // São Paulo
    // Sem zoom por duplo clique: cliques seguidos são relatos, não zoom. Sem rotação: o norte fica sempre para cima.
    doubleClickZoom: false, dragRotate: false, pitchWithRotate: false, touchPitch: false,
  });
  mapa.touchZoomRotate.disableRotation();
  mapa.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'bottom-right');
  mapa.on('click', e => relatar(e.lngLat.lat, e.lngLat.lng));
} else {
  $('mapa').innerHTML = '<p class="vazio" style="padding:12px;text-align:center;margin-top:45vh">Mapa indisponível.</p>';
}

// Mostra uma mensagem no #aviso. Troca o elemento para a animação recomeçar.
function avisar(texto) {
  const novo = document.createElement('div');
  novo.id = 'aviso'; novo.className = 'mostrar'; novo.textContent = texto;
  $('aviso').replaceWith(novo);
}

const erroDeLocalizacao = e =>
  !window.isSecureContext ? 'A localização só funciona com o aplicativo aberto em HTTPS ou em localhost.'
  : e.code === 1 ? 'Permissão de localização negada. Libere nas configurações do navegador para usar a localização.'
  : 'Não foi possível obter sua localização.';

function relatar(lat, lng) {
  enviarRelato({
    autor_id: $('simular').checked ? novoAutor() : autorFixo, tipo: $('tipo').value,
    geolocalizacao: { lat, lng }, timestamp_evento: new Date().toISOString(),
  });
}

// Só falha de rede rejeita: resposta de erro do servidor volta como resposta.
const postarRelato = relato => fetch('/api/relatos', {
  method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(relato), signal: AbortSignal.timeout(5000),
});

async function enviarRelato(relato) {
  let resposta;
  try {
    resposta = await postarRelato(relato);
  } catch {
    await guardarParaDepois(relato);
    return;
  }
  if (!resposta.ok) {
    avisar('Não foi possível enviar o relato: confira o tipo e o local.');
    return;
  }
  const a = await resposta.json(), pessoas = a.n_usuarios === 1 ? '1 pessoa relatou' : `${a.n_usuarios} pessoas diferentes relataram`;
  avisar(`Relato enviado! O alerta nesta área está com credibilidade de ${Math.round(a.score_credibilidade * 100)}% (${nomesNivel[a.nivel]}), porque ${pessoas} na região.`);
  htmx.ajax('GET', '/painel', { source: '#mapa', swap: 'none' });
}

// Relatos feitos sem conexão ficam no aparelho (IndexedDB) e são enviados,
// com o horário original, quando o servidor volta a responder.
let pendentes = 0, enviando = false;
function mostrarPendentes(n) {
  pendentes = n;
  $('pendentes').textContent = n === 0 ? '' : n === 1 ? '1 relato aguardando conexão' : `${n} relatos aguardando conexão`;
}
async function guardarParaDepois(relato) {
  try {
    await fila.guardar(relato);
  } catch {
    avisar('Sem conexão: não foi possível enviar o relato. Tente de novo quando a conexão voltar.');
    return;
  }
  mostrarPendentes(pendentes + 1);
  avisar('Sem conexão: o relato ficou guardado no aparelho e será enviado quando a conexão voltar.');
}
async function enviarPendentes() {
  if (enviando) return;
  enviando = true;
  const guardados = await fila.todos().catch(() => []);
  let enviados = 0;
  for (const relato of guardados) {
    try { await postarRelato(relato); } catch { break; }
    await fila.remover(relato.id);
    enviados++;
  }
  enviando = false;
  mostrarPendentes(guardados.length - enviados);
  if (enviados) avisar(enviados === 1 ? 'Conexão de volta: 1 relato guardado foi enviado.' : `Conexão de volta: ${enviados} relatos guardados foram enviados.`);
}
enviarPendentes();
addEventListener('online', enviarPendentes);
document.addEventListener('htmx:after:swap', () => { if (pendentes) enviarPendentes(); });

$('relatar-aqui').addEventListener('click', () => {
  if (!navigator.geolocation) { avisar('Este aparelho não informa a localização.'); return; }
  navigator.geolocation.getCurrentPosition(
    p => {
      relatar(p.coords.latitude, p.coords.longitude);
      mapa?.flyTo({ center: [p.coords.longitude, p.coords.latitude], zoom: Math.max(mapa.getZoom(), 14) });
    },
    e => avisar(erroDeLocalizacao(e)),
    { enableHighAccuracy: true, timeout: 10000 });
});

// Localização do usuário: ao abrir o painel, o navegador pede permissão e, se
// concedida, o mapa vai até onde a pessoa está. O botão de localização pede de novo.
// O navegador só libera o GPS em HTTPS ou em localhost.
if (mapa) {
  const gps = new maplibregl.GeolocateControl({
    positionOptions: { enableHighAccuracy: true }, fitBoundsOptions: { maxZoom: 14 }, showAccuracyCircle: true,
  });
  mapa.addControl(gps, 'bottom-right');
  // O primeiro pedido é automático: só os erros depois dele, quando o usuário
  // clicou no botão, viram aviso, para não incomodar quem recusou a permissão.
  let automatico = true;
  gps.on('geolocate', () => { automatico = false; });
  gps.on('error', e => {
    if (automatico) { automatico = false; return; }
    avisar(erroDeLocalizacao(e));
  });
  mapa.on('load', () => gps.trigger());
}

// Resolução H3 desenhada em cada zoom: de perto, um hexágono por alerta (a
// resolução 8 do ID); de longe, os alertas vizinhos se juntam no hexágono pai.
// É só apresentação: os alertas continuam sendo os da resolução 8.
const resolucaoDoZoom = z => z >= 11 ? 8 : z >= 9 ? 7 : 6;
const ordemNivel = { INFO: 0, ATENCAO: 1, EMERGENCIA: 2 };

// O servidor manda os contornos em [lat, lng]; o GeoJSON quer [lng, lat] e o anel fechado.
const anelGeoJSON = contorno => { const a = contorno.map(([lat, lng]) => [lng, lat]); return [...a, a[0]]; };
const poligono = (anel, props) => ({ type: 'Feature', properties: props, geometry: { type: 'Polygon', coordinates: [anel] } });
const colecao = features => ({ type: 'FeatureCollection', features });
const centro = anel => {
  const lngs = anel.map(p => p[0]), lats = anel.map(p => p[1]);
  return [(Math.min(...lngs) + Math.max(...lngs)) / 2, (Math.min(...lats) + Math.max(...lats)) / 2];
};

// Camadas do mapa: hexágonos dos alertas (deck.gl), o hexágono tracejado sob o
// mouse e os pluviômetros (MapLibre). Cada hexágono traz a cor e as opacidades;
// o destaque do hexágono sob o mouse (sobMouse) aumenta o preenchimento e a borda.
let sobreposicao = null, celulas = [];
const rgb = hex => [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16));
function desenharHexagonos() {
  sobreposicao.setProps({ layers: [new deck.H3HexagonLayer({
    id: 'alertas', beforeId: 'fantasma-area', data: celulas, getHexagon: d => d.h3,
    pickable: true, filled: true, stroked: true, extruded: false, lineWidthUnits: 'pixels',
    getFillColor: d => [...d.cor, Math.round(255 * (d.opArea + (d.chave === sobMouse ? 0.2 : 0)))],
    getLineColor: d => [...d.cor, Math.round(255 * (d.chave === sobMouse ? 1 : d.opBorda))],
    getLineWidth: d => d.chave === sobMouse ? 4 : 2,
    updateTriggers: { getFillColor: sobMouse, getLineColor: sobMouse, getLineWidth: sobMouse },
  })] });
}
if (mapa) mapa.on('load', () => {
  mapa.addSource('fantasma', { type: 'geojson', data: colecao([]) });
  mapa.addSource('estacoes', { type: 'geojson', data: colecao([]) });
  mapa.addLayer({ id: 'fantasma-area', type: 'fill', source: 'fantasma', paint: { 'fill-color': '#475569', 'fill-opacity': 0.12 } });
  mapa.addLayer({ id: 'fantasma-borda', type: 'line', source: 'fantasma', paint: { 'line-color': '#475569', 'line-width': 2, 'line-dasharray': [2.5, 2.5] } });
  mapa.addLayer({ id: 'estacoes', type: 'circle', source: 'estacoes', paint: {
    'circle-radius': ['get', 'raio'], 'circle-color': '#475569', 'circle-opacity': ['get', 'op'],
    'circle-stroke-color': '#475569', 'circle-stroke-width': 1 } });
  // Os hexágonos dos alertas são uma camada do deck.gl (H3HexagonLayer) intercalada
  // com as do MapLibre, abaixo do hexágono tracejado e dos pluviômetros.
  sobreposicao = new deck.MapboxOverlay({ interleaved: true, layers: [] });
  mapa.addControl(sobreposicao);
  mapaPronto = true;
  desenharMapa();
});

// Destaque do hexágono sob o mouse. Os cliques sobre os hexágonos continuam
// virando relatos; o mapa só consulta qual deles está sob o ponteiro.
// Fora dos alertas, um hexágono tracejado mostra a área que um clique ali
// relataria, na mesma resolução em que o mapa desenha os alertas. Calculado
// no navegador com o h3-js, igual ao servidor (alerta.ChaveDe).
let sobMouse = null, ultimoPonto = null, celulaFantasma = null;
function sobPonteiro() {
  if (!mapaPronto) return;
  const p = ultimoPonto;
  const chave = p && sobreposicao.pickObject({ ...mapa.project(p), layerIds: ['alertas'] })?.object.chave || null;
  destacar(chave);
  const celula = p && !chave && window.h3 ? h3.latLngToCell(p.lat, p.lng, resolucaoDoZoom(mapa.getZoom())) : null;
  if (celula === celulaFantasma) return;
  celulaFantasma = celula;
  mapa.getSource('fantasma').setData(colecao(celula ? [poligono(h3.cellToBoundary(celula, true), {})] : []));
}
function destacar(chave) {
  if (chave === sobMouse) return;
  sobMouse = chave;
  desenharHexagonos();
}
if (mapa) {
  mapa.on('mousemove', e => { ultimoPonto = e.lngLat; sobPonteiro(); });
  mapa.on('mouseout', () => { ultimoPonto = null; sobPonteiro(); });
  // Dica do pluviômetro ao passar o mouse.
  const dica = new maplibregl.Popup({ closeButton: false, closeOnClick: false, className: 'dica-pluviometro', offset: 8 });
  mapa.on('mousemove', 'estacoes', e => { const f = e.features[0]; dica.setLngLat(f.geometry.coordinates).setHTML(f.properties.html).addTo(mapa); });
  mapa.on('mouseleave', 'estacoes', () => dica.remove());
}

// Popup de um alerta, aberto pela lista de alertas (flyTo + abrirPopup).
let popup = null, popupChave = null, popupHtml = null;
function abrirPopup(chave) {
  const a = areasAtuais[chave];
  if (!a) return;
  popup?.remove();
  popupChave = chave; popupHtml = a.html;
  popup = new maplibregl.Popup({ maxWidth: '300px' }).setLngLat(a.centro).setHTML(a.html).addTo(mapa);
  popup.on('close', () => { if (popupChave === chave) popupChave = null; });
}

// desenharMapa sincroniza as camadas com #dados-mapa, que o servidor
// renderiza (já escapado) junto com os outros painéis.
function desenharMapa() {
  if (!mapaPronto) return;
  const res = resolucaoDoZoom(mapa.getZoom()), grupos = {};
  for (const el of $('dados-mapa').querySelectorAll('[data-alerta]')) {
    const d = el.dataset, chave = res === 8 ? d.alerta : d['area' + res];
    (grupos[chave] ??= { contorno: res === 8 ? d.contorno : d['contorno' + res], els: [] }).els.push(el);
  }
  const novas = {}, hexagonos = [];
  for (const chave in grupos) {
    const { contorno, els } = grupos[chave];
    // A área tem a cor do pior nível entre os alertas ativos; sem nenhum ativo, fica apagada.
    const ativos = els.filter(el => 'ativo' in el.dataset), ativo = ativos.length > 0;
    const nivel = (ativo ? ativos : els).map(el => el.dataset.nivel).reduce((x, y) => ordemNivel[y] > ordemNivel[x] ? y : x);
    const html = els.length === 1 ? els[0].innerHTML
      : `<b>${els.length} alertas nesta região</b> (${ativos.length} ${ativos.length === 1 ? 'ativo' : 'ativos'}) · aproxime o mapa para separá-los<hr>`
        + els.map(el => el.innerHTML).join('<hr>');
    const anel = anelGeoJSON(JSON.parse(contorno));
    novas[chave] = { html, centro: centro(anel) };
    hexagonos.push({ chave, h3: chave.split('_')[0], cor: rgb(cores[nivel]), opArea: ativo ? 0.35 : 0.08, opBorda: ativo ? 1 : 0.3 });
  }
  areasAtuais = novas;
  celulas = hexagonos;
  if (sobMouse && !(sobMouse in novas)) sobMouse = null;
  desenharHexagonos();
  if (popupChave) {
    const a = novas[popupChave];
    if (!a) popup.remove();
    else if (a.html !== popupHtml) popup.setHTML(popupHtml = a.html);
  }

  const pontos = [];
  for (const el of $('dados-mapa').querySelectorAll('[data-sensor]')) {
    const d = el.dataset, intens = Math.min(+d.chuva / 50, 1);
    pontos.push({ type: 'Feature', geometry: { type: 'Point', coordinates: [+d.lng, +d.lat] },
      properties: { raio: 3 + intens * 4, op: 0.25 + intens * 0.7, html: el.innerHTML } });
  }
  mapa.getSource('estacoes').setData(colecao(pontos));
  // Os hexágonos novos só podem ser consultados (pickObject) depois de desenhados.
  mapa.once('idle', sobPonteiro);
}
desenharMapa();
document.addEventListener('htmx:after:swap', desenharMapa);
if (mapa) mapa.on('zoomend', desenharMapa);

// Clicar num alerta da lista leva o mapa até ele e abre o popup.
// O zoom mínimo de 14 garante que o alerta esteja no próprio hexágono, e não
// junto com os vizinhos, quando o popup abrir.
$('feed').addEventListener('click', e => {
  const cartao = e.target.closest('.alerta'), id = cartao && cartao.id.slice('alerta-'.length), dados = id && $('mapa-' + id);
  if (!mapaPronto || !dados || e.target.closest('button, details')) return;
  mapa.once('moveend', () => { desenharMapa(); abrirPopup(id); });
  mapa.flyTo({ center: centro(anelGeoJSON(JSON.parse(dados.dataset.contorno))), zoom: Math.max(mapa.getZoom(), 14) });
});

$('abrir-ajuda').addEventListener('click', () => $('ajuda').showModal());
$('ajuda').addEventListener('click', e => { if (e.target === $('ajuda')) $('ajuda').close(); });

// O morph fecha os <details> que o servidor renderiza fechados. Guardamos os
// que o usuário abriu e reabrimos a cada atualização, antes de a tela pintar.
const abertos = new Set();
document.addEventListener('toggle', e => {
  if (e.target.id) e.target.open ? abertos.add(e.target.id) : abertos.delete(e.target.id);
}, true);
document.addEventListener('htmx:after:swap', () => { for (const id of abertos) { const d = $(id); if (d) d.open = true; } });

const semConexao = () => { $('conexao').textContent = '⚠ este servidor não está respondendo'; };
document.addEventListener('htmx:error', semConexao);
document.addEventListener('htmx:response:error', semConexao);

if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js');

let convite = null;
addEventListener('beforeinstallprompt', e => { e.preventDefault(); convite = e; $('instalar').hidden = false; });
$('instalar').addEventListener('click', () => { $('instalar').hidden = true; convite.prompt(); });
