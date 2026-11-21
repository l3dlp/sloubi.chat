/* app.js — front KISS, toute la logique de vérif est côté serveur */

'use strict';

/* ========== State & DOM ========== */
const CHAN = document.body.dataset.chan || 'chante';
const THEME = document.body.dataset.theme || 'taverne';
const LOCKED = document.body.hasAttribute('data-locked'); // pas encore le mot de passe
const API = `/api/${encodeURIComponent(CHAN)}`;

let sloubiCount = 0;
let messageCount = 0;    // forêt et moulin : on compte tous les messages
let countedUpTo = -1;    // id du dernier message déjà compté par /info (le backlog ne recompte pas) ; -1 = pas encore reçu
let deferredPrompt = null;
let ws = null;
let reconnectTimer = null;
let leaving = false;     // on quitte la page : pas de reconnexion
let reloadDelay = 0;     // le créateur vient de changer un réglage : il voit « Changé ✓ » avant le rechargement
let lastSeenId = 0; // le backlog est renvoyé à chaque reconnexion : on dédoublonne par id

const chatMessages = document.getElementById('chatMessages');
const chatInput = document.getElementById('chatInput');
const sendButton = document.getElementById('sendButton');
const sloubiCountElement = document.getElementById('sloubiCount');

const settingsButton = document.getElementById('settingsButton');
const settingsModal = document.getElementById('settingsModal');
const closeSettings = document.getElementById('closeSettings');

const cheatModeToggle = document.getElementById('cheatModeToggle');
const cheatButton = document.getElementById('cheatButton');
const quickSloubi = document.getElementById('quickSloubi');

const pwaInstallSection = document.getElementById('pwaInstallSection');
const installButton = document.getElementById('installButton');

const nicknameInput = document.getElementById('nicknameInput');
const copyLink = document.getElementById('copyLink');

/* ========== Stockage local (peut être indisponible : navigation privée, etc.) ========== */
const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch {} },
};

/* ========== Clé du créateur (donnée à la fondation, ou par le lien #cle=…) ========== */
const OWNER_KEY = `sloubi_owner_${CHAN}`;
(function adoptOwnerLink() {
  const m = location.hash.match(/^#cle=([A-Za-z0-9_-]{20,64})$/);
  if (!m) return;
  store.set(OWNER_KEY, m[1]);
  history.replaceState(null, '', location.pathname + location.search); // la clé ne reste pas dans l'historique
})();
let ownerKey = store.get(OWNER_KEY) || '';
function keepOwnerKey(j) {
  if (!j || typeof j.ownerKey !== 'string' || !j.ownerKey) return;
  ownerKey = j.ownerKey;
  store.set(OWNER_KEY, ownerKey);
  place.refresh(true);
}

/* ========== Client ID (persisté) ========== */
const CLIENT_ID_KEY = 'sloubi_client_id';
function genClientId() {
  try { if (crypto && typeof crypto.randomUUID === 'function') return crypto.randomUUID(); } catch {}
  return 'c_' + Date.now() + '_' + Math.random().toString(36).slice(2);
}
const clientId = (() => {
  let id = store.get(CLIENT_ID_KEY);
  if (!id) {
    id = genClientId();
    store.set(CLIENT_ID_KEY, id);
  }
  return id;
})();

/* ========== Nickname (gardé localement, validé par le serveur) ========== */
let nickname = store.get('sloubi_nickname') || '';
if (nickname.toLowerCase() === 'anonyme') nickname = ''; // l'ancien nom par défaut ne compte pas
nicknameInput.value = nickname;

/* ========== Pas de pseudo, pas de message : la saisie demande d'abord le nom ========== */
const BASE_PLACEHOLDER = chatInput.placeholder;
const hasNick = () => !!nickname;
function nickMode() {
  const need = !hasNick();
  document.body.classList.toggle('needs-nick', need);
  chatInput.maxLength = need ? 24 : 500;
  chatInput.setAttribute('aria-label', need ? 'Ton pseudo' : 'Votre message');
  if (need) chatInput.placeholder = 'D’abord, ton pseudo (2 à 24 caractères)…';
  else if (THEME === 'taverne') chatInput.placeholder = `Écrivez « sloubi ${sloubiCount + 1} » pour compter...`;
  else chatInput.placeholder = BASE_PLACEHOLDER;
}
window.addEventListener('sloubi:need-nick', () => { nickMode(); chatInput.focus(); });

const NICK_ERRORS = {
  tooshort: 'Pseudo trop court (2 caractères minimum).',
  toolong: 'Pseudo trop long (24 caractères maximum).',
  chars: 'Pseudo refusé : lettres, chiffres, espace et _ - . \' seulement.',
  forbidden: 'Ce pseudo est réservé.',
};

// le serveur renvoie la forme canonique : c'est elle qu'on garde
async function setNickname(raw) {
  try {
    const r = await fetch('/api/nick', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ nickname: raw })
    });
    if (r.status === 429) { addSystemMessage('Doucement ! Réessaie dans un instant.'); return false; }
    const j = await r.json();
    if (!j.ok) {
      addSystemMessage(NICK_ERRORS[j.reason] || 'Pseudo refusé.');
      nicknameInput.value = nickname;
      return false;
    }
    const first = !hasNick();
    nickname = j.nickname;
    store.set('sloubi_nickname', nickname);
    nicknameInput.value = nickname;
    addSystemMessage(first ? `Bienvenue, ${nickname}. Tu peux écrire.` : `Tu t’appelles désormais ${nickname}.`);
    nickMode();
    return true;
  } catch {
    addSystemMessage('Serveur indisponible.');
    return false;
  }
}

/* ========== Les boutons « Changer » : Changer → En cours… → Changé ✓, puis ils reviennent ========== */
async function runButton(btn, action, done = 'Changé') {
  const idle = btn.textContent;
  btn.disabled = true;
  btn.dataset.state = 'busy';
  btn.textContent = 'En cours…';
  let ok = false;
  try { ok = !!(await action()); } catch { ok = false; }
  if (ok) {
    btn.dataset.state = 'done';
    btn.textContent = `${done} ✓`;
    await new Promise((r) => setTimeout(r, 1600));
  }
  btn.dataset.state = '';
  btn.textContent = idle;
  btn.disabled = false;
  return ok;
}

const nickSave = document.getElementById('nickSave');
const nickChanged = () => { const v = nicknameInput.value.trim(); return !!v && v !== nickname; };
const syncNickSave = () => { if (nickSave.dataset.state !== 'busy' && nickSave.dataset.state !== 'done') nickSave.disabled = !nickChanged(); };
nicknameInput.addEventListener('input', syncNickSave);
nicknameInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); if (!nickSave.disabled) nickSave.click(); } });
nickSave.addEventListener('click', async () => {
  if (!nickChanged()) return;
  await runButton(nickSave, () => setNickname(nicknameInput.value.trim()));
  syncNickSave();
});

/* ========== Salons récents (repris par l'accueil) ========== */
(function rememberChannel() {
  let recent = [];
  try { recent = JSON.parse(store.get('sloubi_recent') || '[]'); } catch {}
  if (!Array.isArray(recent)) recent = [];
  recent = [CHAN, ...recent.filter((c) => c !== CHAN)].slice(0, 6);
  store.set('sloubi_recent', JSON.stringify(recent));
})();

/* ========== UI helpers ========== */
// on ne fait défiler que si le lecteur était déjà en bas : quelqu'un qui relit plus haut n'est pas arraché
const nearBottom = () => chatMessages.scrollHeight - chatMessages.scrollTop - chatMessages.clientHeight < 120;
const toBottom = () => { chatMessages.scrollTop = chatMessages.scrollHeight; };

function addSystemMessage(text) {
  const stick = nearBottom();
  const div = document.createElement('div');
  div.className = 'system-message';
  div.textContent = text;
  chatMessages.appendChild(div);
  lastRow = null;
  if (stick) toBottom();
}

/* ========== Mes messages (à droite), mes likes ========== */
// le serveur ne dit pas qui a écrit quoi : on retient l'id de nos propres messages, dans ce navigateur
const MINE_KEY = `sloubi_mine_${CHAN}`;
const LIKED_KEY = `sloubi_liked_${CHAN}`;
const loadSet = (k) => { try { const a = JSON.parse(store.get(k) || '[]'); return new Set(Array.isArray(a) ? a : []); } catch { return new Set(); } };
const saveSet = (k, set) => store.set(k, JSON.stringify([...set].slice(-600)));
const mine = loadSet(MINE_KEY);
const liked = loadSet(LIKED_KEY);
const pendingMine = []; // ce qu'on vient d'envoyer : on le reconnaît s'il revient par le WebSocket avant la réponse

// la réponse arrive après la bulle : on la passe à droite
function markMine(id) {
  const row = chatMessages.querySelector(`.msg-row[data-id="${id}"]`);
  if (!row || row.classList.contains('mine')) return;
  row.classList.replace('theirs', 'mine');
  row.querySelector('.avatar')?.remove();
  toBottom();
}

/* ========== Les liens : cliquables, dans un nouvel onglet, et on compte le total des clics ========== */
// même règle que extractLinks (reactions.go) : l'ordre des liens sert d'index pour les clics
const LINK_RE = /(?:https?:\/\/|www\.)[^\s<>"'`]+/gi;

function splitLinks(text) {
  const parts = [];
  let last = 0, idx = 0;
  for (const m of text.matchAll(LINK_RE)) {
    let url = m[0].replace(/[.,;:!?»”’']+$/, '');
    if (url.endsWith(')') && !url.includes('(')) url = url.slice(0, -1);
    if (url.length <= 4) continue;
    if (idx >= 20) break;
    parts.push(text.slice(last, m.index));
    parts.push({ url, i: idx++ });
    last = m.index + url.length;
  }
  parts.push(text.slice(last));
  return parts;
}

function prettyUrl(url) {
  const s = url.replace(/^https?:\/\//i, '').replace(/\/$/, '');
  return s.length > 48 ? `${s.slice(0, 45)}…` : s;
}

// le texte d'un message : des liens, et (en forêt) des #mots ; jamais d'HTML
function fillText(el, text, msg) {
  const tags = [];
  for (const part of splitLinks(text)) {
    if (typeof part === 'string') {
      if (THEME === 'foret') tags.push(...fillWithTags(el, part));
      else el.append(part);
      continue;
    }
    let href = /^www\./i.test(part.url) ? `https://${part.url}` : part.url;
    try {
      const u = new URL(href);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error('protocole');
      href = u.href;
    } catch { el.append(part.url); continue; }
    const a = document.createElement('a');
    a.href = href;
    a.target = '_blank';
    a.rel = 'noopener noreferrer nofollow ugc';
    a.textContent = prettyUrl(part.url);
    a.title = href;
    a.dataset.i = String(part.i);
    el.appendChild(a);
    const n = document.createElement('span');
    n.className = 'link-count';
    n.dataset.i = String(part.i);
    setLinkCount(n, msg.clicks?.[part.i] || 0);
    el.appendChild(n);
    if (msg.id) a.addEventListener('click', () => countClick(msg.id, part.i));
  }
  return [...new Set(tags)].slice(0, 5);
}

function setLinkCount(el, n) {
  el.textContent = n > 0 ? `↗ ${n}` : '';
  el.title = n > 0 ? `${n} ${n > 1 ? 'clics' : 'clic'}` : '';
}

function countClick(id, i) {
  fetch(`${API}/click`, {
    method: 'POST',
    keepalive: true, // l'onglet du lien s'ouvre : la requête part quand même
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ id, i }),
  }).catch(() => {});
}

function onClickCount(id, i, n) {
  const el = chatMessages.querySelector(`.msg-row[data-id="${id}"] .link-count[data-i="${i}"]`);
  if (el) setLinkCount(el, n);
}

/* ========== Les likes : n'importe quel message, un total, et le navigateur se souvient du sien ========== */
function likeButton(msg) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'like-btn';
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('d', 'M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z');
  svg.appendChild(path);
  const n = document.createElement('span');
  n.className = 'like-n';
  b.append(svg, n);
  setLikes(b, msg.likes || 0, liked.has(msg.id));
  b.addEventListener('click', () => toggleLike(msg.id, b));
  return b;
}

function setLikes(b, count, mineLiked) {
  b.querySelector('.like-n').textContent = count > 0 ? String(count) : '';
  b.classList.toggle('has-likes', count > 0);
  b.setAttribute('aria-pressed', String(!!mineLiked));
  b.setAttribute('aria-label', `${mineLiked ? 'Retirer mon cœur' : 'Mettre un cœur'}${count > 0 ? ` (${count})` : ''}`);
  b.dataset.count = String(count);
}

async function toggleLike(id, b) {
  if (!id) return;
  const on = !liked.has(id);
  const before = Number(b.dataset.count || 0);
  // tout de suite à l'écran ; le serveur confirme le total juste après
  if (on) liked.add(id); else liked.delete(id);
  saveSet(LIKED_KEY, liked);
  setLikes(b, Math.max(0, before + (on ? 1 : -1)), on);
  if (on && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
    b.classList.remove('pop');
    void b.offsetWidth;
    b.classList.add('pop');
    setTimeout(() => b.classList.remove('pop'), 250);
  }
  try {
    const r = await fetch(`${API}/like`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ id, delta: on ? 1 : -1 }),
    });
    if (!r.ok) throw new Error(String(r.status));
    const j = await r.json();
    setLikes(b, j.likes, on);
  } catch {
    if (on) liked.delete(id); else liked.add(id);
    saveSet(LIKED_KEY, liked);
    setLikes(b, before, !on);
  }
}

function onLikeCount(id, n) {
  const b = chatMessages.querySelector(`.msg-row[data-id="${id}"] .like-btn`);
  if (b) setLikes(b, n, liked.has(id));
  window.dispatchEvent(new CustomEvent('sloubi:likes', { detail: { id, likes: n } }));
}

/* ========== Les fichiers dans le fil ========== */
const fileURL = (f, opts = '') => `${API}/files/${encodeURIComponent(f.id)}${opts}`;
function humanSize(n) {
  if (n < 1024) return `${n} o`;
  const u = ['Ko', 'Mo', 'Go', 'To'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return `${n.toFixed(n < 10 ? 1 : 0).replace('.', ',')} ${u[i]}`;
}

function fileBlock(f) {
  if (f.kind === 'image') {
    const img = document.createElement('img');
    img.className = 'file-img';
    img.loading = 'lazy';
    img.decoding = 'async';
    img.src = fileURL(f, f.thumb ? '?thumb=1' : '');
    img.alt = f.name;
    // la place est réservée d'après les vraies proportions : le fil ne saute pas quand l'image arrive
    if (f.w && f.h) img.style.setProperty('--ratio', String(f.w / f.h));
    img.addEventListener('load', () => { if (nearBottom() || img.dataset.stick) toBottom(); }, { once: true });
    img.addEventListener('click', () => window.dispatchEvent(new CustomEvent('sloubi:view', { detail: f })));
    return img;
  }
  if (f.kind === 'video') {
    const v = document.createElement('video');
    v.className = 'file-video';
    v.controls = true;
    v.preload = 'metadata';
    v.playsInline = true;
    v.src = `${fileURL(f)}#t=0.1`; // la première image sert d'affiche
    return v;
  }
  if (f.kind === 'audio') {
    const a = document.createElement('audio');
    a.className = 'file-audio';
    a.controls = true;
    a.preload = 'metadata';
    a.src = fileURL(f);
    return a;
  }
  return docCard(f);
}

function docCard(f) {
  const a = document.createElement('a');
  a.className = 'file-doc';
  a.href = fileURL(f, '?dl=1');
  a.setAttribute('download', f.name);
  const ext = document.createElement('span');
  ext.className = 'file-ext';
  ext.textContent = (f.name.match(/\.([a-z0-9]{1,5})$/i)?.[1] || 'doc').slice(0, 4);
  const txt = document.createElement('span');
  const name = document.createElement('span');
  name.className = 'file-name';
  name.textContent = f.name;
  const size = document.createElement('span');
  size.className = 'file-size';
  size.textContent = `${humanSize(f.size)} · Télécharger`;
  txt.append(name, size);
  a.append(ext, txt);
  return a;
}
window.sloubiFile = { fileBlock, docCard, fileURL, humanSize, fillText, countClick, prettyUrl }; // pour library.js

/* ========== Une journée, une heure ========== */
let lastRow = null;
let lastDay = '';
const dayLabel = (d) => {
  const today = new Date(), y = new Date(Date.now() - 864e5);
  if (d.toDateString() === today.toDateString()) return 'Aujourd’hui';
  if (d.toDateString() === y.toDateString()) return 'Hier';
  return d.toLocaleDateString('fr-FR', { weekday: 'long', day: 'numeric', month: 'long', year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric' });
};
const hueOf = (s) => { let h = 0; for (const ch of s || '') h = (h * 31 + ch.codePointAt(0)) % 360; return h; };

function renderMessage(msg) {
  // msg = { id, nickname, message, sloubiNumber, timestamp, likes, clicks, file } — texte brut uniquement
  const stick = nearBottom();
  const when = new Date(msg.timestamp || Date.now());
  const day = when.toDateString();
  if (day !== lastDay) {
    const sep = document.createElement('div');
    sep.className = 'day-sep';
    sep.textContent = dayLabel(when);
    chatMessages.appendChild(sep);
    lastDay = day;
    lastRow = null;
  }

  // à moi ? (envoyé depuis ce navigateur)
  const pend = pendingMine.findIndex((p) => p.nick === msg.nickname && p.text === msg.message);
  if (pend >= 0) {
    pendingMine.splice(pend, 1);
    mine.add(msg.id);
    saveSet(MINE_KEY, mine);
  }
  const isMine = mine.has(msg.id);

  const row = document.createElement('div');
  row.className = `msg-row ${isMine ? 'mine' : 'theirs'}`;
  row.dataset.id = String(msg.id);
  row.dataset.nick = msg.nickname || '';
  row.dataset.ts = String(when.getTime());
  // plusieurs messages de suite de la même personne : une seule pastille, un seul nom
  if (lastRow && lastRow.dataset.nick === row.dataset.nick && lastRow.classList.contains(isMine ? 'mine' : 'theirs')
      && when.getTime() - Number(lastRow.dataset.ts) < 5 * 60e3) {
    row.classList.add('cont');
  }

  if (!isMine) {
    const av = document.createElement('span');
    av.className = 'avatar';
    av.setAttribute('aria-hidden', 'true');
    av.textContent = [...(msg.nickname || '?')][0];
    av.style.setProperty('--hue', String(hueOf(msg.nickname)));
    row.appendChild(av);
  }

  const col = document.createElement('div');
  col.className = 'msg-col';
  const article = document.createElement('div');
  article.className = 'message';

  const who = document.createElement('span');
  who.className = 'message-nickname';
  who.textContent = msg.nickname || 'Anonyme';
  article.appendChild(who);

  const body = document.createElement('span');
  // un fichier : son nom est déjà dans la carte, pas besoin de le répéter
  const tags = msg.file ? [] : fillText(body, msg.message || '', msg);
  article.appendChild(body);
  if (msg.file) {
    const block = fileBlock(msg.file);
    if (stick) block.dataset.stick = '1'; // on était en bas : on y reste quand l'image a fini de charger
    article.appendChild(block);
  }

  // marquage sloubi visuel (la taverne seulement)
  if (THEME === 'taverne' && typeof msg.sloubiNumber === 'number') {
    article.classList.add('sloubi-message');
    article.dataset.sloubi = String(msg.sloubiNumber);
  }
  // double-clic (ou double tape) sur la bulle : un cœur, comme dans une messagerie
  article.addEventListener('dblclick', (e) => {
    if (e.target.closest('a, video, audio')) return;
    const b = row.querySelector('.like-btn');
    if (b && !liked.has(msg.id)) toggleLike(msg.id, b);
  });

  const meta = document.createElement('div');
  meta.className = 'msg-meta';
  const time = document.createElement('time');
  time.dateTime = when.toISOString();
  time.textContent = when.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' });
  meta.append(time, likeButton(msg));

  col.append(article, meta);
  row.appendChild(col);
  chatMessages.appendChild(row);
  lastRow = row;
  if (stick || isMine) toBottom();

  // pour les effets des décors (*-fx.js) ; "live" = pas un message du backlog
  const live = Date.now() - Date.parse(msg.timestamp || 0) < 15000;
  window.dispatchEvent(new CustomEvent('sloubi:message', { detail: { ...msg, live, tags, el: article } }));
  if (tags.length && live) biomes.grow(tags);
}

/* ========== Forêt : les #mots sont des microbiotes ========== */
// même règle que le serveur (extractTags) : un # en début de mot, 2 à 32 lettres, chiffres, _ ou -
const TAG_RE = /(?<![\p{L}\p{N}_&#])#([\p{L}\p{N}_-]{2,32})/gu;
const normTag = (t) => t.replace(/^[-_]+|[-_]+$/g, '').normalize('NFC').toLowerCase();

function fillWithTags(el, text) {
  const tags = [];
  let last = 0;
  for (const m of text.matchAll(TAG_RE)) {
    const tag = normTag(m[1]);
    if ([...tag].length < 2) continue;
    el.append(text.slice(last, m.index));
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'tag';
    b.textContent = m[0];
    b.addEventListener('click', () => codex.open(tag));
    el.appendChild(b);
    last = m.index + m[0].length;
    if (!tags.includes(tag)) tags.push(tag);
  }
  el.append(text.slice(last));
  return tags.slice(0, 5);
}

const biomes = (() => {
  const nav = document.getElementById('biomes');
  const counts = new Map();
  function render() {
    if (!nav) return;
    const list = [...counts].sort((a, b) => b[1] - a[1]).slice(0, 24);
    nav.replaceChildren(...list.map(([tag, n]) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'biome';
      b.dataset.tag = tag;
      b.append(`#${tag}`);
      const c = document.createElement('span');
      c.className = 'biome-count';
      c.textContent = String(n);
      b.appendChild(c);
      b.addEventListener('click', () => codex.open(tag));
      return b;
    }));
    nav.hidden = list.length === 0;
  }
  async function load() {
    if (!nav) return;
    try {
      const r = await fetch(`${API}/microbiotes`, { cache: 'no-store' });
      if (!r.ok) return;
      const j = await r.json();
      counts.clear();
      for (const t of j.tags || []) counts.set(t.tag, t.count);
      render();
    } catch { /* la forêt attendra */ }
  }
  function grow(tags) {
    if (!nav) return;
    for (const t of tags) counts.set(t, (counts.get(t) || 0) + 1);
    render();
    for (const t of tags) nav.querySelector(`[data-tag="${CSS.escape(t)}"]`)?.classList.add('bloom');
  }
  return { load, grow };
})();

// l'encyclopédie d'un microbiote : tout ce qui a été dit sous ce #mot
const codex = (() => {
  const el = document.getElementById('codex');
  if (!el) return { open() {} };
  const title = document.getElementById('codexTitle');
  const sub = document.getElementById('codexSub');
  const list = document.getElementById('codexList');
  let opener = null;
  const close = () => { el.hidden = true; opener?.focus(); };
  document.getElementById('codexClose').addEventListener('click', close);
  el.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.preventDefault(); close(); } });
  async function open(tag) {
    opener = document.activeElement;
    title.textContent = `#${tag}`;
    sub.textContent = 'On soulève la mousse…';
    list.replaceChildren();
    el.hidden = false;
    el.focus();
    try {
      const r = await fetch(`${API}/microbiotes/${encodeURIComponent(tag)}`, { cache: 'no-store' });
      if (!r.ok) throw new Error(String(r.status));
      const j = await r.json();
      const msgs = j.messages || [];
      sub.textContent = msgs.length
        ? `${msgs.length} ${msgs.length > 1 ? 'feuillets' : 'feuillet'} dans ce microbiote`
        : 'Rien n’a encore poussé ici.';
      list.replaceChildren(...msgs.map((m) => {
        const li = document.createElement('li');
        const who = document.createElement('span');
        who.className = 'message-nickname';
        who.textContent = m.nickname || 'Anonyme';
        const when = document.createElement('time');
        when.className = 'codex-when';
        when.dateTime = m.timestamp;
        when.textContent = new Date(m.timestamp).toLocaleDateString('fr-FR', { day: 'numeric', month: 'short', year: 'numeric' });
        const body = document.createElement('p');
        fillWithTags(body, m.message || '');
        li.append(who, when, body);
        return li;
      }));
    } catch {
      sub.textContent = 'Le sentier est coupé. Réessaie.';
    }
  }
  return { open };
})();

/* ========== Compteurs : les sloubis à la taverne, tous les messages ailleurs ========== */
const counterSub = document.getElementById('counterSub');
const scoreEl = document.getElementById('score');
const scoreLabel = scoreEl?.querySelector('.score-label')?.textContent || '';
const plural = (n, one, many) => `${n} ${n > 1 ? many : one}`;
// every : un palier tous les N messages ; sub : la ligne sous le compteur ; milestone : ce qu'on dit au palier
const COUNTERS = {
  foret: {
    every: 10,
    sub: (n) => (n >= 10 ? `${plural(Math.floor(n / 10), 'arbre a', 'arbres ont')} pris racine` : 'la terre est prête'),
    milestone: () => '🌳 Un arbre a pris racine.',
  },
  moulin: {
    every: 10,
    sub: (n) => plural(Math.floor(n / 10), 'sac de farine', 'sacs de farine').replace(/^0 sacs/, '0 sac'),
    milestone: () => '🌾 La meule a rempli un sac de farine.',
  },
  cyber: {
    every: 10,
    sub: (n) => `niveau d’alerte ${Math.floor(n / 10)}`,
    milestone: (n) => `SYS> Mise à jour du firmware v${Math.floor(n / 10)}.0. Ne débranchez rien. Surtout pas vous.`,
  },
  kawaii: {
    every: 10,
    sub: (n) => (n >= 10 ? plural(Math.floor(n / 10), 'arc-en-ciel', 'arcs-en-ciel') : 'pas encore d’arc-en-ciel'),
    milestone: () => '🌈 Un arc-en-ciel ! Tout le monde a gagné. Même toi.',
  },
  bd: {
    every: 6, // six cases par planche
    sub: (n) => `planche ${Math.floor(n / 6) + 1}, case ${(n % 6) + 1}`,
    milestone: (n) => `Fin de la planche ${n / 6}. La suite au prochain numéro…`,
  },
};

function bump() {
  sloubiCountElement.style.transform = 'scale(1.2)';
  setTimeout(() => (sloubiCountElement.style.transform = 'scale(1)'), 180);
}

function setMessageCount(n, live) {
  const before = messageCount;
  messageCount = n;
  sloubiCountElement.textContent = String(n);
  const c = COUNTERS[THEME] || COUNTERS.moulin;
  counterSub.textContent = c.sub(n);
  scoreEl.title = `${n} ${scoreLabel} · ${c.sub(n)}`;
  counterSub.hidden = false;
  window.dispatchEvent(new CustomEvent('sloubi:count', { detail: { count: n, live } }));
  if (!live) return;
  bump();
  if (Math.floor(n / c.every) > Math.floor(before / c.every)) {
    addSystemMessage(c.milestone(Math.floor(n / c.every) * c.every));
    window.dispatchEvent(new CustomEvent('sloubi:milestone', { detail: { count: n } }));
  }
}

function setSloubiCount(n) {
  if (typeof n !== 'number' || isNaN(n)) return;
  sloubiCount = n;
  sloubiCountElement.textContent = String(sloubiCount);
  bump();
  nickMode();
}

/* ========== Send ========== */
const ERRORS = {
  400: 'Message vide ou trop long (500 caractères max).',
  403: 'La porte s’est refermée. Il faut le mot de passe.',
  404: 'Ce lieu n’existe pas.',
  429: 'Doucement ! Trop de messages d’un coup.',
};

// Un seul envoi à la fois, et la saisie est vidée tout de suite : si le réseau traîne,
// marteler Entrée n'empile plus 4 copies du même message.
let sending = false;
async function sendMessage() {
  const message = String(chatInput.value || '').trim();
  if (!message || sending) return;

  // /nick <pseudo> : change de pseudo, rien n'est publié
  const nick = message.match(/^\/nick(?:\s+(.*))?$/i);
  if (nick) {
    if (!nick[1]) { addSystemMessage('Usage : /nick <pseudo>'); return; }
    chatInput.value = '';
    if (!(await setNickname(nick[1]))) chatInput.value = message;
    return;
  }

  // pas encore de pseudo : ce qu'on tape, c'est son nom
  if (!hasNick()) {
    chatInput.value = '';
    if (!(await setNickname(message))) chatInput.value = message;
    return;
  }

  sending = true;
  chatInput.value = '';
  pendingMine.push({ nick: nickname, text: message });
  const restore = () => { if (!chatInput.value) chatInput.value = message; }; // pour réessayer
  try {
    const r = await fetch(`${API}/messages`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ clientId, nickname, message })
    });
    if (r.status === 403) { location.reload(); return; }
    if (r.status === 400) {
      const j = await r.json().catch(() => ({}));
      if (j.error === 'nickname required') {
        // le pseudo gardé n'est plus accepté (réservé depuis, par exemple) : on en redemande un
        nickname = '';
        store.set('sloubi_nickname', '');
        nicknameInput.value = '';
        addSystemMessage(NICK_ERRORS[j.reason] || 'Choisis d’abord un pseudo.');
        nickMode();
        restore();
        return;
      }
    }
    if (!r.ok) {
      addSystemMessage(ERRORS[r.status] || 'Personne n’a compris.');
      restore();
      return;
    }
    const j = await r.json().catch(() => null);
    if (j && j.id) { mine.add(j.id); saveSet(MINE_KEY, mine); markMine(j.id); }
    keepOwnerKey(j);
  } catch (e) {
    addSystemMessage("Serveur indisponible.");
    console.warn('POST messages failed', e);
    restore();
  } finally {
    sending = false;
  }
}

/* ========== Keyboard & buttons ========== */
sendButton.addEventListener('click', sendMessage);
// le clavier reste ouvert quand on touche « Envoyer » (sinon il se ferme, et la page saute)
sendButton.addEventListener('mousedown', (e) => e.preventDefault());
sendButton.addEventListener('touchend', (e) => {
  if (document.activeElement !== chatInput) return; // clavier fermé : le clic normal suffit
  e.preventDefault(); // pas de clic, pas de changement de focus : le clavier ne bouge pas
  sendMessage();
});

/* ========== Le clavier : le fil reste en bas, et faire défiler le fil range le clavier ========== */
let pinned = true; // le lecteur était-il en bas du fil ?
chatMessages.addEventListener('scroll', () => { pinned = nearBottom(); }, { passive: true });
window.addEventListener('sloubi:viewport', () => { if (pinned) toBottom(); });

let dragY = null;
chatMessages.addEventListener('touchstart', (e) => { dragY = e.touches[0].clientY; }, { passive: true });
chatMessages.addEventListener('touchmove', (e) => {
  if (dragY === null || document.activeElement !== chatInput) return;
  if (e.touches[0].clientY - dragY > 14) { chatInput.blur(); dragY = null; } // on remonte dans le fil : on range le clavier
}, { passive: true });
chatMessages.addEventListener('touchend', () => { dragY = null; }, { passive: true });
chatInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    sendMessage();
  }
});

copyLink?.addEventListener('click', async () => {
  const url = `${location.origin}/${CHAN}`;
  try {
    await navigator.clipboard.writeText(url);
    addSystemMessage(`🔗 Lien copié : ${url}`);
  } catch {
    addSystemMessage(`🔗 ${url}`);
  }
});

/* ========== Settings modal ========== */
settingsButton.addEventListener('click', () => {
  settingsModal.classList.add('active');
  place.refresh();
});
closeSettings.addEventListener('click', () => settingsModal.classList.remove('active'));
settingsModal.addEventListener('click', (e) => {
  if (e.target === settingsModal) settingsModal.classList.remove('active');
});

/* ========== Cheat mode (front-side helper only, taverne seulement) ========== */
cheatModeToggle?.addEventListener('change', (e) => {
  const on = !!e.target.checked;
  cheatButton.hidden = !on;
  if (on) addSystemMessage('Mode Triche : bouton rapide activé (la validation reste côté serveur).');
});
quickSloubi?.addEventListener('click', () => {
  chatInput.value = `sloubi ${sloubiCount + 1}`;
  sendMessage();
});

/* ========== Réglages du lieu (créateur seulement, ou fondation d'un lieu neuf) ========== */
const place = (() => {
  const form = document.getElementById('placeSection');
  const title = document.getElementById('placeTitle');
  const pw = document.getElementById('placePassword');
  const pwLabel = document.getElementById('placePwLabel');
  const pwSave = document.getElementById('placePwSave');
  const remove = document.getElementById('placeRemove');
  const linkBtn = document.getElementById('placeLink');
  const note = document.getElementById('placeNote');
  const pick = document.getElementById('themePick');
  let info = null;

  const say = (t) => { note.textContent = t; };

  function show() {
    const owner = !!(info && info.owner);
    const claimable = !!(info && info.claimable);
    form.hidden = !(owner || claimable);
    if (form.hidden) return;
    title.textContent = owner ? 'Ton lieu' : 'Fonde ce lieu';
    form.classList.toggle('is-owner', owner);
    pwLabel.textContent = info.locked ? 'Nouveau mot de passe' : 'Mot de passe';
    pw.placeholder = info.locked ? 'gardé tel quel' : 'aucun, lieu ouvert';
    remove.hidden = !info.locked;
    linkBtn.hidden = !owner;
  }

  async function refresh(force) {
    if (info && !force) { show(); return info; }
    try {
      const r = await fetch(`${API}/info`, { cache: 'no-store', headers: ownerKey ? { 'x-owner-key': ownerKey } : {} });
      if (!r.ok) return null;
      info = await r.json();
      show();
      return info;
    } catch { return null; }
  }

  // un réglage : renvoie la réponse du serveur, ou null (et dit pourquoi)
  async function post(body) {
    say('');
    try {
      const r = await fetch(`${API}/settings`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ ownerKey, ...body }),
      });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) {
        say({ 403: 'Seul le créateur peut régler ce lieu.', 429: 'Doucement, réessaie dans un instant.', 400: 'Réglage refusé.' }[r.status]
          || 'Réglage impossible.');
        return null;
      }
      if (j.ownerKey) { ownerKey = j.ownerKey; store.set(OWNER_KEY, ownerKey); }
      return j;
    } catch {
      say('Serveur indisponible.');
      return null;
    }
  }

  // le décor ou la serrure change : le serveur fait recharger tout le monde, et il le fait AVANT de nous
  // répondre. On pose donc le délai avant la requête, pour voir « Changé ✓ » avant le rechargement.
  async function postChange(body, ms) {
    reloadDelay = ms;
    const j = await post(body);
    if (!j) { reloadDelay = 0; return null; }
    setTimeout(() => location.reload(), ms); // au cas où le WebSocket ne nous le dirait pas
    return j;
  }

  // un clic sur un décor : appliqué tout de suite
  pick.addEventListener('change', async (e) => {
    const input = e.target;
    if (input.name !== 'theme') return;
    if (input.value === THEME && info?.exists) return;
    const card = input.closest('.theme-card');
    const radios = [...pick.querySelectorAll('input')];
    radios.forEach((r) => { r.disabled = true; });
    card.classList.add('is-busy');
    say('En cours…');
    const j = await postChange({ theme: input.value }, 1200);
    card.classList.remove('is-busy');
    if (!j) {
      radios.forEach((r) => { r.disabled = false; r.checked = r.value === THEME; });
      return;
    }
    card.classList.add('is-done');
    say(`Changé ✓ Le décor passe ${{ taverne: 'en taverne', foret: 'en forêt', moulin: 'au moulin' }[j.theme] || ''}…`);
  });

  const syncPwSave = () => { if (!pwSave.dataset.state) pwSave.disabled = !pw.value; };
  pw.addEventListener('input', syncPwSave);
  pw.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); if (!pwSave.disabled) pwSave.click(); } });
  form.addEventListener('submit', (e) => e.preventDefault());

  pwSave.addEventListener('click', async () => {
    if ([...pw.value].length < 4) { say('4 caractères minimum pour le mot de passe.'); pw.focus(); return; }
    const value = pw.value;
    const ok = await runButton(pwSave, async () => {
      // tout le monde repasse par la porte ; nous avons le cookie, on rentre directement
      const j = await postChange({ password: value }, 1900);
      if (j) pw.value = '';
      return !!j;
    });
    if (!ok) syncPwSave();
  });

  remove.addEventListener('click', async () => {
    await runButton(remove, async () => {
      return !!(await postChange({ removePassword: true }, 1900));
    }, 'Retiré');
  });

  linkBtn.addEventListener('click', async () => {
    const url = `${location.origin}/${CHAN}#cle=${ownerKey}`;
    try {
      await navigator.clipboard.writeText(url);
      say('Lien copié. Garde-le pour toi : il ouvre les réglages sur un autre appareil.');
    } catch {
      say(url);
    }
  });

  return { refresh };
})();

/* ========== PWA install ========== */
window.addEventListener('beforeinstallprompt', (e) => {
  e.preventDefault();
  deferredPrompt = e;
  pwaInstallSection.hidden = false;
});
installButton.addEventListener('click', async () => {
  if (!deferredPrompt) {
    if (isIOS() && !isInStandaloneMode()) {
      alert('Pour installer sur iOS : bouton Partager → “Sur l’écran d’accueil”.');
    }
    return;
  }
  deferredPrompt.prompt();
  try { await deferredPrompt.userChoice; } catch {}
  deferredPrompt = null;
  pwaInstallSection.hidden = true;
});
function isIOS() { return /iPhone|iPad|iPod/.test(navigator.userAgent) && !window.MSStream; }
function isInStandaloneMode() { return window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone; }
if (isInStandaloneMode()) { pwaInstallSection.hidden = true; }

/* ========== Ambient messages (fun only) ========== */
const ambientMessages = {
  taverne: [
    "🍺 On entend le bruit des chopes qui s'entrechoquent...",
    "🔥 Le feu crépite dans la cheminée...",
    "🎵 Un ménestrel joue une ballade au loin...",
    "👥 Des voyageurs discutent à voix basse...",
    "🌙 La nuit est tombée sur la Bretagne...",
    "⚔️ Un chevalier entre dans la taverne...",
    "🐀 Un rat traverse furtivement la pièce..."
  ],
  foret: [
    "🍄 Sous la mousse, les racines se passent des nouvelles...",
    "🐝 Une abeille butine la haie de petits fruits...",
    "🌿 Le vent fait bruisser la canopée...",
    "🍂 Une feuille tombe et nourrit la terre...",
    "🦉 Une chouette observe depuis le vieux chêne...",
    "💧 La rosée perle sur les fougères...",
    "🌱 Une graine oubliée commence à germer..."
  ],
  moulin: [
    "🌊 L'eau clapote contre la roue...",
    "🐟 Une truite saute dans le bief...",
    "🪵 La roue grince, puis reprend son rythme...",
    "🦆 Un canard descend le courant sans se presser...",
    "🌾 Le meunier secoue un sac de farine...",
    "🐸 Une grenouille s'installe sur une pierre...",
    "🍃 Le saule laisse traîner ses branches dans l'eau..."
  ],
  cyber: [
    "SYS> Je ne lis pas vos messages. (Je mens.)",
    "SYS> Votre curseur a bougé. Je l'ai noté quelque part.",
    "SYS> Bande VHS en fin de course. Rembobinez par politesse.",
    "SYS> Quelqu'un a encore collé un tract sur mon écran.",
    "SYS> Mot de passe administrateur : ********. Ah, ça s'affiche pas ?",
    "SYS> Ce salon n'existe pas. Vous non plus, d'ailleurs.",
    "SYS> Photocopieuse en surchauffe. Continuez, ça lui fait du bien."
  ],
  kawaii: [
    "Le bouton envoyer te fait coucou. Fais-lui coucou aussi.",
    "Un nuage est passé. Il a dit bonjour. C'était un bon nuage.",
    "Petit rappel : tu fais de ton mieux, et c'est très bien.",
    "Le compteur a fait une sieste. Il va mieux.",
    "Une étoile est tombée. On l'a rattrapée, elle va bien.",
    "Le bouton envoyer voudrait qu'on lui écrive quelque chose. N'importe quoi."
  ],
  bd: [
    "Pendant ce temps, à l'autre bout de la ville…",
    "Plus tard…",
    "Note de l'auteur : cette case n'a pas été dessinée, faute de budget.",
    "Soudain !",
    "Le lendemain, à la même heure…",
    "Note de l'éditeur : les bulles sont garanties sans fautes. Enfin, presque.",
    "Pendant ce temps, le dessinateur cherche une gomme."
  ],
}[THEME] || [];
function scheduleAmbient() {
  if (ambientMessages.length && Math.random() > 0.3) addSystemMessage(ambientMessages[Math.floor(Math.random() * ambientMessages.length)]);
  setTimeout(scheduleAmbient, 15000 + Math.random() * 25000);
}
if (!LOCKED) setTimeout(scheduleAmbient, 10000);

/* ========== Post-it (contenu partagé, position et état propres au visiteur) ========== */
const postit = (() => {
  const navPin = document.getElementById('navPostit');
  const el = document.getElementById('postit');
  const handle = document.getElementById('postitHandle');
  const text = document.getElementById('postitText');
  const count = document.getElementById('postitCount');
  const closeBtn = document.getElementById('postitClose');
  const pin = document.getElementById('postitPin');
  const KEY = `sloubi_postit_${CHAN}`;
  const MARGIN = 8;

  let st = null;
  try { st = JSON.parse(store.get(KEY) || 'null'); } catch {}
  if (!st || typeof st !== 'object') {
    // par défaut : en bas à droite, au-dessus de la saisie (le compteur reste visible) ; punaise sur mobile
    st = window.innerWidth >= 700
      ? { x: window.innerWidth - 240 - 24, y: window.innerHeight - 230 - 110, open: true }
      : { x: window.innerWidth - 58, y: 70, open: false }; // punaise sous l'engrenage
  }

  let remote = '';       // dernière version serveur
  let dirty = false;     // modifié localement, pas encore envoyé
  let saveTimer = null;

  const save = () => store.set(KEY, JSON.stringify(st));

  function clamp() {
    // on borne l'élément visible : le parchemin ouvert, ou la seule punaise
    const box = st.open ? el : pin;
    const w = box.offsetWidth || (st.open ? 240 : 46), h = box.offsetHeight || (st.open ? 200 : 46);
    st.x = Math.min(Math.max(MARGIN, st.x), Math.max(MARGIN, window.innerWidth - w - MARGIN));
    st.y = Math.min(Math.max(MARGIN, st.y), Math.max(MARGIN, window.innerHeight - h - MARGIN));
    // jamais par-dessus la barre d'icônes : on doit toujours pouvoir y toucher « Post-it »
    const bar = navPin?.parentElement.getBoundingClientRect();
    if (bar && st.open) {
      if (bar.width > bar.height) st.y = Math.max(st.y, bar.bottom + MARGIN); // en haut (portrait)
      else st.x = Math.max(st.x, bar.right + MARGIN); // à gauche (paysage)
    }
  }

  function place() {
    clamp();
    el.style.transform = `translate(${st.x}px, ${st.y}px)`;
    pin.style.transform = `translate(${st.x}px, ${st.y}px)`;
  }

  function setOpen(open, focus) {
    st.open = open;
    el.hidden = !open;
    pin.hidden = open;
    if (open) { pin.classList.remove('has-news'); navPin?.classList.remove('has-news'); }
    navPin?.setAttribute('aria-pressed', String(open));
    place();
    save();
    if (focus) (open ? text : (navPin || pin)).focus(); // la punaise est cachée : on revient à l'icône de la barre
  }

  function updateCount() { count.textContent = String([...text.value].length); }

  /* déplacement à la souris et au doigt */
  let drag = null;
  handle.addEventListener('pointerdown', (e) => {
    if (e.target === closeBtn) return;
    drag = { id: e.pointerId, dx: e.clientX - st.x, dy: e.clientY - st.y };
    handle.setPointerCapture(e.pointerId);
    el.classList.add('dragging');
  });
  handle.addEventListener('pointermove', (e) => {
    if (!drag || e.pointerId !== drag.id) return;
    st.x = e.clientX - drag.dx;
    st.y = e.clientY - drag.dy;
    place();
  });
  const endDrag = (e) => {
    if (!drag || e.pointerId !== drag.id) return;
    drag = null;
    el.classList.remove('dragging');
    save();
  };
  handle.addEventListener('pointerup', endDrag);
  handle.addEventListener('pointercancel', endDrag);

  /* déplacement au clavier */
  handle.addEventListener('keydown', (e) => {
    const step = e.shiftKey ? 40 : 10;
    const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    if (!moves[e.key]) return;
    e.preventDefault();
    st.x += moves[e.key][0];
    st.y += moves[e.key][1];
    place();
    save();
  });
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { e.preventDefault(); setOpen(false, true); }
  });

  closeBtn.addEventListener('click', () => setOpen(false, true));
  pin.addEventListener('click', () => setOpen(true, true));
  // l'icône « Post-it » de la barre l'ouvre et le referme
  navPin?.addEventListener('click', () => setOpen(!st.open, st.open ? false : true));
  window.addEventListener('resize', place);

  /* édition partagée : le dernier qui écrit gagne */
  async function push() {
    clearTimeout(saveTimer);
    if (!dirty) return;
    dirty = false;
    try {
      const r = await fetch(`${API}/postit`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ clientId, text: text.value })
      });
      if (r.status === 429) { dirty = true; saveTimer = setTimeout(push, 3000); return; }
      if (r.status === 403) { location.reload(); return; }
      if (!r.ok) { addSystemMessage('Le post-it n’a pas pu être enregistré.'); return; }
      keepOwnerKey(await r.json().catch(() => null));
    } catch {
      dirty = true;
      saveTimer = setTimeout(push, 5000);
    }
  }
  text.addEventListener('input', () => {
    dirty = true;
    updateCount();
    clearTimeout(saveTimer);
    saveTimer = setTimeout(push, 1200);
  });
  text.addEventListener('blur', () => {
    if (dirty) push();
    else if (text.value !== remote) { text.value = remote; updateCount(); }
  });

  // mise à jour reçue par le WS ; on n'écrase pas ce que le visiteur est en train de taper
  function receive(p) {
    const next = String(p.text || '');
    if (next === remote) return;
    remote = next;
    if (document.activeElement !== text && !dirty) { text.value = remote; updateCount(); }
    if (!st.open) { pin.classList.add('has-news'); navPin?.classList.add('has-news'); }
  }

  if (!LOCKED) setOpen(!!st.open, false);
  return { receive };
})();

/* ========== WebSocket ========== */
function connectWebSocket() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${proto}//${location.host}/ws/${encodeURIComponent(CHAN)}`);

  ws.onopen = () => {
    addSystemMessage(`Connecté à /${CHAN}.`);
    window.dispatchEvent(new CustomEvent('sloubi:ws-open'));
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  };

  ws.onmessage = (ev) => {
    let data;
    try { data = JSON.parse(ev.data); } catch (e) { console.warn('WS parse error', e); return; }
    if (data.type === 'postit') { postit.receive(data); return; }
    if (data.type === 'call') { window.dispatchEvent(new CustomEvent('sloubi:call', { detail: data })); return; } // call.js
    // le créateur a changé le décor ou la serrure : le serveur nous déconnecte, on recharge
    if (data.type === 'room') { leaving = true; setTimeout(() => location.reload(), reloadDelay); return; }
    if (data.type === 'like') { onLikeCount(data.id, data.likes); return; }
    if (data.type === 'click') { onClickCount(data.id, data.i, data.clicks); return; }
    if (typeof data.id !== 'number' || data.id <= lastSeenId) return;
    lastSeenId = data.id;
    renderMessage(data);
    if (THEME !== 'taverne') {
      if (countedUpTo >= 0 && data.id > countedUpTo) { countedUpTo = data.id; setMessageCount(messageCount + 1, true); }
      return;
    }
    // Si le serveur a validé un sloubi → on prend sa valeur comme vérité
    if (typeof data.sloubiNumber === 'number') setSloubiCount(data.sloubiNumber);
  };

  ws.onerror = () => { /* bruit blanc */ };

  ws.onclose = () => {
    ws = null;
    window.dispatchEvent(new CustomEvent('sloubi:ws-closed'));
    if (leaving) return; // fermeture voulue : on quitte la page
    addSystemMessage('Déconnecté. Reconnexion…');
    reconnectTimer = setTimeout(reconnect, 2000 + Math.random() * 2000);
  };
}

// call.js parle au serveur par ce même WebSocket (signalisation des appels)
window.addEventListener('sloubi:send', (e) => {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(e.detail));
});

// avant de se reconnecter, on vérifie que la porte est toujours ouverte pour nous
async function reconnect() {
  const info = await place.refresh(true);
  if (info && !info.access) { location.reload(); return; }
  connectWebSocket();
}

// Quand on quitte la page, on ferme le WS nous-mêmes. Sinon il survit en arrière-plan
// (cache arrière/avant du navigateur) et bloque les navigations suivantes plusieurs secondes.
window.addEventListener('pagehide', () => {
  leaving = true;
  clearTimeout(reconnectTimer);
  reconnectTimer = null;
  if (ws) ws.close(1000);
});
window.addEventListener('pageshow', (e) => {
  if (!e.persisted) return; // premier affichage : déjà connecté
  if (LOCKED) return;
  leaving = false;
  reconnect(); // page restaurée depuis le cache : on se reconnecte (le backlog est dédoublonné)
  syncCounter();
});

/* ========== La porte (lieu gardé par un mot de passe) ========== */
function openGate() {
  const gate = document.getElementById('gate');
  const input = document.getElementById('gatePassword');
  const error = document.getElementById('gateError');
  for (const sel of ['#chatMessages', '.room-bottom', '.score', '#biomes']) {
    const el = document.querySelector(sel);
    if (el) el.hidden = true;
  }
  gate.hidden = false;
  input.focus();
  gate.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!input.value) return;
    error.textContent = '';
    try {
      const r = await fetch(`${API}/unlock`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ password: input.value }),
      });
      if (r.ok) { location.reload(); return; }
      error.textContent = r.status === 429 ? 'Trop d’essais. Reviens dans un moment.' : 'Ce n’est pas le bon mot de passe.';
      gate.classList.remove('shake');
      void gate.offsetWidth;
      gate.classList.add('shake');
      input.select();
    } catch {
      error.textContent = 'Serveur indisponible.';
    }
  });
}

/* ========== Boot niceties ========== */
sloubiCountElement.style.transition = 'transform .18s ease';

// le compteur au chargement : le dernier sloubi à la taverne, le nombre de messages ailleurs
async function syncCounter() {
  const info = await place.refresh(true);
  if (!info || !info.access) return;
  if (THEME === 'taverne') { setSloubiCount(info.sloubi || 0); return; }
  countedUpTo = Math.max(countedUpTo, info.lastId || 0);
  setMessageCount(info.messages || 0, false);
}

if (LOCKED) {
  openGate();
} else {
  nickMode();
  if (!hasNick()) addSystemMessage('Avant d’écrire, choisis ton pseudo : tape-le en bas.');
  chatInput.focus();
  connectWebSocket();
  syncCounter();
  if (THEME === 'foret') biomes.load();
}
