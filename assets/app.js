/* app.js — front KISS, toute la logique de vérif est côté serveur */

'use strict';

/* ========== State & DOM ========== */
let sloubiCount = 0;
let deferredPrompt = null;
let ws = null;
let reconnectTimer = null;

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

/* ========== Client ID (persisté) ========== */
const CLIENT_ID_KEY = 'sloubi_client_id';
function genClientId() {
  try { if (crypto && typeof crypto.randomUUID === 'function') return crypto.randomUUID(); } catch {}
  return 'c_' + Date.now() + '_' + Math.random().toString(36).slice(2);
}
const clientId = (() => {
  let id = localStorage.getItem(CLIENT_ID_KEY);
  if (!id) {
    id = genClientId();
    localStorage.setItem(CLIENT_ID_KEY, id);
  }
  return id;
})();

/* ========== Nickname (local only) ========== */
let nickname = localStorage.getItem('sloubi_nickname') || '';
nicknameInput.value = nickname;
nicknameInput.addEventListener('input', (e) => {
  nickname = String(e.target.value || '').trim();
  localStorage.setItem('sloubi_nickname', nickname);
});

/* ========== UI helpers ========== */
function addSystemMessage(text) {
  const div = document.createElement('div');
  div.className = 'system-message';
  div.textContent = text;
  chatMessages.appendChild(div);
  chatMessages.scrollTop = chatMessages.scrollHeight;
}

function renderMessage(msg) {
  // msg = { clientId, nickname, message, html, gif, sloubiNumber, timestamp }
  const article = document.createElement('div');
  article.className = 'message';

  // head
  const who = document.createElement('span');
  who.className = 'message-nickname';
  who.textContent = `${msg.nickname || 'Anonyme'}: `;
  article.appendChild(who);

  // body (HTML déjà sanitizé côté serveur)
  const body = document.createElement('span');
  if (msg.html) body.innerHTML = msg.html;
  else body.textContent = msg.message || '';
  article.appendChild(body);

  // gif (optionnel)
  if (msg.gif) {
    const wrap = document.createElement('div');
    wrap.style.marginTop = '6px';
    const low = String(msg.gif).toLowerCase();
    if (low.endsWith('.mp4')) {
      const v = document.createElement('video');
      v.src = msg.gif; v.controls = true; v.playsInline = true;
      wrap.appendChild(v);
    } else {
      const img = document.createElement('img');
      img.src = msg.gif; img.alt = 'gif';
      img.style.maxWidth = '100%';
      wrap.appendChild(img);
    }
    article.appendChild(wrap);
  }

  // marquage sloubi visuel
  if (typeof msg.sloubiNumber === 'number') {
    article.classList.add('sloubi-message');
  }

  chatMessages.appendChild(article);
  chatMessages.scrollTop = chatMessages.scrollHeight;
}

function setSloubiCount(n) {
  if (typeof n !== 'number' || isNaN(n)) return;
  sloubiCount = n;
  sloubiCountElement.textContent = String(sloubiCount);
  sloubiCountElement.style.transform = 'scale(1.2)';
  setTimeout(() => (sloubiCountElement.style.transform = 'scale(1)'), 180);
}

/* ========== Send ========== */
async function sendMessage() {
  const message = String(chatInput.value || '').trim();
  if (!message && !nickname) return; // rien à envoyer, pas même un pseudo…
  try {
    await fetch('/verif.json', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        clientId,
        nickname: nickname || 'Anonyme',
        message,
        timestamp: new Date().toISOString()
        // pas de gif ici: ton HTML actuel n'a pas de picker
      })
    });
  } catch (e) {
    addSystemMessage("Serveur indisponible.");
    console.warn('POST /verif.json failed', e);
  }
  chatInput.value = '';
}

/* ========== Keyboard & buttons ========== */
sendButton.addEventListener('click', sendMessage);
chatInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    sendMessage();
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
  cheatButton.style.display = on ? 'block' : 'none';
  if (on) addSystemMessage('Mode Triche : bouton rapide activé (la validation reste côté serveur).');
});
quickSloubi.addEventListener('click', () => {
  chatInput.value = 'sloubi';
  sendMessage();
});

/* ========== PWA install ========== */
window.addEventListener('beforeinstallprompt', (e) => {
  e.preventDefault();
  deferredPrompt = e;
  pwaInstallSection.style.display = 'block';
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
  pwaInstallSection.style.display = 'none';
});
function isIOS() { return /iPhone|iPad|iPod/.test(navigator.userAgent) && !window.MSStream; }
function isInStandaloneMode() { return window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone; }
if (isInStandaloneMode()) { pwaInstallSection.style.display = 'none'; }

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

/* ========== WebSocket ========== */
function connectWebSocket() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/ws`;
  ws = new WebSocket(url);

  ws.onopen = () => {
    addSystemMessage('Connecté au serveur.');
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  };

  ws.onmessage = (ev) => {
    try {
      const data = JSON.parse(ev.data);
      // Affiche la ligne
      renderMessage(data);
      // Si le serveur a incrémenté un sloubi → on prend sa valeur comme vérité
      if (typeof data.sloubiNumber === 'number') {
        setSloubiCount(data.sloubiNumber);
      }
    } catch (e) {
      console.warn('WS parse error', e);
    }
  };

  ws.onerror = () => { /* bruit blanc */ };

  ws.onclose = () => {
    addSystemMessage('Déconnecté. Reconnexion…');
    reconnectTimer = setTimeout(connectWebSocket, 2000);
  };
}
connectWebSocket();

/* ========== Boot niceties ========== */
chatInput.focus();
sloubiCountElement.style.transition = 'transform .18s ease';

/* Optionnel : récupérer le dernier sloubi au chargement pour sync visuelle */
(async function syncLastSloubi() {
  try {
    const r = await fetch('/last-sloubi.json', { cache: 'no-store' });
    const j = await r.json();
    if (j && j.exists && typeof j.number === 'number') {
      setSloubiCount(j.number);
    } else {
      setSloubiCount(0);
    }
  } catch { /* ignore */ }
})();