/* app.js — front KISS, toute la logique de vérif est côté serveur */

'use strict';

/* ========== State & DOM ========== */
const CHAN = document.body.dataset.chan || 'chante';
const API = `/api/${encodeURIComponent(CHAN)}`;

let sloubiCount = 0;
let deferredPrompt = null;
let ws = null;
let reconnectTimer = null;
let leaving = false;     // on quitte la page : pas de reconnexion
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
nicknameInput.value = nickname;

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
    nickname = j.nickname;
    store.set('sloubi_nickname', nickname);
    nicknameInput.value = nickname;
    addSystemMessage(`Tu t’appelles désormais ${nickname}.`);
    return true;
  } catch {
    addSystemMessage('Serveur indisponible.');
    return false;
  }
}

nicknameInput.addEventListener('change', () => {
  const v = nicknameInput.value.trim();
  if (v && v !== nickname) setNickname(v);
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
function addSystemMessage(text) {
  const div = document.createElement('div');
  div.className = 'system-message';
  div.textContent = text;
  chatMessages.appendChild(div);
  chatMessages.scrollTop = chatMessages.scrollHeight;
}

function renderMessage(msg) {
  // msg = { id, channel, nickname, message, sloubiNumber, timestamp } — texte brut uniquement
  const article = document.createElement('div');
  article.className = 'message';

  const who = document.createElement('span');
  who.className = 'message-nickname';
  who.textContent = msg.nickname || 'Anonyme';
  article.appendChild(who);

  const body = document.createElement('span');
  body.textContent = msg.message || '';
  article.appendChild(body);

  // marquage sloubi visuel
  if (typeof msg.sloubiNumber === 'number') {
    article.classList.add('sloubi-message');
    article.dataset.sloubi = String(msg.sloubiNumber);
  }

  chatMessages.appendChild(article);
  chatMessages.scrollTop = chatMessages.scrollHeight;

  // pour les effets de la taverne enfumée (room-fx.js) ; "live" = pas un message du backlog
  const live = Date.now() - Date.parse(msg.timestamp || 0) < 15000;
  window.dispatchEvent(new CustomEvent('sloubi:message', { detail: { ...msg, live } }));
}

function setSloubiCount(n) {
  if (typeof n !== 'number' || isNaN(n)) return;
  sloubiCount = n;
  sloubiCountElement.textContent = String(sloubiCount);
  sloubiCountElement.style.transform = 'scale(1.2)';
  setTimeout(() => (sloubiCountElement.style.transform = 'scale(1)'), 180);
  chatInput.placeholder = `Écrivez « sloubi ${sloubiCount + 1} » pour compter...`;
}

/* ========== Send ========== */
const ERRORS = {
  400: 'Message vide ou trop long (500 caractères max).',
  404: 'Cette taverne n’existe pas.',
  429: 'Doucement, aubergiste ! Trop de messages d’un coup.',
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

  sending = true;
  chatInput.value = '';
  const restore = () => { if (!chatInput.value) chatInput.value = message; }; // pour réessayer
  try {
    const r = await fetch(`${API}/messages`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ clientId, nickname: nickname || 'Anonyme', message })
    });
    if (!r.ok) {
      addSystemMessage(ERRORS[r.status] || 'Le tavernier n’a pas compris.');
      restore();
    }
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
settingsButton.addEventListener('click', () => settingsModal.classList.add('active'));
closeSettings.addEventListener('click', () => settingsModal.classList.remove('active'));
settingsModal.addEventListener('click', (e) => {
  if (e.target === settingsModal) settingsModal.classList.remove('active');
});

/* ========== Cheat mode (front-side helper only) ========== */
cheatModeToggle.addEventListener('change', (e) => {
  const on = !!e.target.checked;
  cheatButton.hidden = !on;
  if (on) addSystemMessage('Mode Triche : bouton rapide activé (la validation reste côté serveur).');
});
quickSloubi.addEventListener('click', () => {
  chatInput.value = `sloubi ${sloubiCount + 1}`;
  sendMessage();
});

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
const ambientMessages = [
  "🍺 On entend le bruit des chopes qui s'entrechoquent...",
  "🔥 Le feu crépite dans la cheminée...",
  "🎵 Un ménestrel joue une ballade au loin...",
  "👥 Des voyageurs discutent à voix basse...",
  "🌙 La nuit est tombée sur la Bretagne...",
  "⚔️ Un chevalier entre dans la taverne...",
  "🐀 Un rat traverse furtivement la pièce..."
];
function scheduleAmbient() {
  if (Math.random() > 0.3) addSystemMessage(ambientMessages[Math.floor(Math.random() * ambientMessages.length)]);
  setTimeout(scheduleAmbient, 15000 + Math.random() * 25000);
}
setTimeout(scheduleAmbient, 10000);

/* ========== Post-it (contenu partagé, position et état propres au visiteur) ========== */
const postit = (() => {
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
    if (open) pin.classList.remove('has-news');
    place();
    save();
    if (focus) (open ? text : pin).focus();
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
      if (!r.ok) addSystemMessage('Le post-it n’a pas pu être enregistré.');
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
    if (!st.open) pin.classList.add('has-news');
  }

  setOpen(!!st.open, false);
  return { receive };
})();

/* ========== WebSocket ========== */
function connectWebSocket() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${proto}//${location.host}/ws/${encodeURIComponent(CHAN)}`);

  ws.onopen = () => {
    addSystemMessage(`Connecté à la taverne /${CHAN}.`);
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  };

  ws.onmessage = (ev) => {
    let data;
    try { data = JSON.parse(ev.data); } catch (e) { console.warn('WS parse error', e); return; }
    if (data.type === 'postit') { postit.receive(data); return; }
    if (typeof data.id !== 'number' || data.id <= lastSeenId) return;
    lastSeenId = data.id;
    renderMessage(data);
    // Si le serveur a validé un sloubi → on prend sa valeur comme vérité
    if (typeof data.sloubiNumber === 'number') setSloubiCount(data.sloubiNumber);
  };

  ws.onerror = () => { /* bruit blanc */ };

  ws.onclose = () => {
    ws = null;
    if (leaving) return; // fermeture voulue : on quitte la page
    addSystemMessage('Déconnecté. Reconnexion…');
    reconnectTimer = setTimeout(connectWebSocket, 2000 + Math.random() * 2000);
  };
}
connectWebSocket();

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
  leaving = false;
  connectWebSocket(); // page restaurée depuis le cache : on se reconnecte (le backlog est dédoublonné)
  syncLastSloubi();
});

/* ========== Boot niceties ========== */
chatInput.focus();
sloubiCountElement.style.transition = 'transform .18s ease';

/* Récupérer le dernier sloubi au chargement pour sync visuelle */
async function syncLastSloubi() {
  try {
    const r = await fetch(`${API}/last-sloubi`, { cache: 'no-store' });
    const j = await r.json();
    setSloubiCount(j && j.exists && typeof j.number === 'number' ? j.number : 0);
  } catch { /* ignore */ }
}
syncLastSloubi();
