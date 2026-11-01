/* home.js — accueil : on tape le nom de sa taverne, vert si c'est bon, rouge sinon.
   Le serveur fait foi (/api/check) ; ici on ne fait que réagir vite. */

'use strict';

const CHAN_RE = /^[a-z0-9]+([._-][a-z0-9]+)*$/;
const MAX_LEN = 32;

const form = document.getElementById('prompt');
const input = document.getElementById('chanInput');
const mirror = document.getElementById('chanMirror');
const cursor = document.getElementById('chanCursor');
const hint = document.getElementById('chanHint');
const line = form.querySelector('.prompt-line');
const recentBox = document.getElementById('recent');

const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
const cache = new Map(); // nom → réponse de /api/check
let state = 'empty';
let pending = null;      // AbortController de la requête en cours
let debounce = null;

const HINTS = {
  empty: 'Tape le nom de ta taverne',
  wait: '…',
  checking: 'Le tavernier consulte ses registres…',
  new: 'Entrée ↵ fonder la taverne',
  exists: 'Entrée ↵ rejoindre la taverne',
  start: 'GAME OVER · commence par une lettre ou un chiffre',
  double: 'GAME OVER · un seul séparateur à la fois',
  chars: 'GAME OVER · lettres, chiffres, . _ - seulement',
  toolong: 'GAME OVER · 32 caractères maximum',
  forbidden: 'GAME OVER · ce nom est réservé',
  slow: 'Trop d’essais. Le tavernier fait une pause, reviens dans un moment.',
  offline: 'Le tavernier ne répond pas. Réessaie.',
};

/* ========== Mesure : le champ suit la largeur du texte, et rétrécit pour tenir ========== */
function fit() {
  mirror.textContent = input.value || input.placeholder;
  line.style.setProperty('--k', '1');
  input.style.width = `${mirror.offsetWidth + 2}px`;
  const avail = form.clientWidth;
  const width = line.getBoundingClientRect().width;
  const k = width > avail ? avail / width : 1;
  line.style.setProperty('--k', k.toFixed(3));
  input.style.width = `${mirror.offsetWidth + 2}px`;
  // curseur bloc seulement quand le caret natif est en fin de saisie
  const atEnd = input.selectionStart === input.value.length && input.selectionEnd === input.value.length;
  line.classList.toggle('caret-native', !atEnd);
}

/* ========== États visuels ========== */
function setState(next, hintKey) {
  const prev = state;
  state = next;
  document.body.dataset.state = next;
  hint.textContent = HINTS[hintKey || next] || '';
  fit();
  if (next === 'ok' && prev !== 'ok') sparkle();
  if (next === 'bad' && prev !== 'bad') shake();
}

function shake() {
  if (reducedMotion) return;
  line.classList.remove('shake');
  void line.offsetWidth; // relance l'animation
  line.classList.add('shake');
}

function sparkle() {
  if (reducedMotion) return;
  const box = line.getBoundingClientRect();
  for (let i = 0; i < 14; i++) {
    const s = document.createElement('span');
    s.className = 'spark';
    s.style.left = `${box.left + Math.random() * box.width}px`;
    s.style.top = `${box.top + box.height * (0.3 + Math.random() * 0.5)}px`;
    s.style.setProperty('--dx', `${(Math.random() - 0.5) * 80}px`);
    s.style.setProperty('--dy', `${-40 - Math.random() * 90}px`);
    s.style.animationDelay = `${Math.random() * 0.25}s`;
    document.body.appendChild(s);
    s.addEventListener('animationend', () => s.remove());
  }
}

/* ========== Validation ========== */
// règles locales : immédiates, mêmes que le serveur
function localVerdict(v) {
  if (!v) return { state: 'empty' };
  if (v.length > MAX_LEN) return { state: 'bad', hint: 'toolong' };
  if (/[^a-z0-9._-]/.test(v)) return { state: 'bad', hint: 'chars' };
  if (/^[._-]/.test(v)) return { state: 'bad', hint: 'start' };
  if (/[._-]{2}/.test(v)) return { state: 'bad', hint: 'double' };
  if (/[._-]$/.test(v)) return { state: 'wait' }; // on attend la suite
  if (CHAN_RE.test(v)) return { state: 'check' };
  return { state: 'bad', hint: 'chars' };
}

function applyCheck(res) {
  if (res.ok) setState('ok', res.exists ? 'exists' : 'new');
  else setState('bad', res.reason === 'forbidden' ? 'forbidden' : res.reason === 'toolong' ? 'toolong' : 'chars');
}

async function remoteCheck(v) {
  if (cache.has(v)) { applyCheck(cache.get(v)); return; }
  pending?.abort();
  pending = new AbortController();
  const mine = pending;
  setState('wait', 'checking');
  try {
    const r = await fetch(`/api/check/${encodeURIComponent(v)}`, { signal: mine.signal, cache: 'no-store' });
    if (r.status === 429) { setState('wait', 'slow'); return; }
    if (!r.ok) throw new Error(String(r.status));
    const res = await r.json();
    cache.set(v, res);
    if (input.value === v) applyCheck(res);
  } catch (e) {
    if (e.name !== 'AbortError' && input.value === v) setState('wait', 'offline');
  }
}

function validate() {
  const v = input.value;
  clearTimeout(debounce);
  pending?.abort();
  const verdict = localVerdict(v);
  if (verdict.state === 'check') {
    if (cache.has(v)) applyCheck(cache.get(v));
    else {
      setState('wait');
      debounce = setTimeout(() => remoteCheck(v), 300);
    }
    return;
  }
  setState(verdict.state, verdict.hint);
}

/* ========== Saisie ========== */
input.addEventListener('input', () => {
  // minuscules, et on ignore tout ce qui n'est pas a-z 0-9 . _ -
  const cleaned = input.value.toLowerCase().replace(/[^a-z0-9._-]/g, '').slice(0, MAX_LEN);
  if (cleaned !== input.value) {
    input.value = cleaned;
    input.setSelectionRange(cleaned.length, cleaned.length);
  }
  validate();
});

['keyup', 'click', 'select', 'focus', 'blur'].forEach((ev) => input.addEventListener(ev, fit));
document.addEventListener('selectionchange', () => { if (document.activeElement === input) fit(); });
window.addEventListener('resize', fit);
input.addEventListener('focus', () => line.classList.add('focused'));
input.addEventListener('blur', () => line.classList.remove('focused'));

form.addEventListener('submit', (e) => {
  e.preventDefault();
  if (state === 'ok') {
    location.href = `/${input.value}`;
    return;
  }
  if (state === 'wait' && localVerdict(input.value).state === 'check') {
    // Entrée avant la fin du debounce : on vérifie tout de suite
    remoteCheck(input.value).then(() => { if (state === 'ok') location.href = `/${input.value}`; });
    return;
  }
  shake();
});

/* ========== Tavernes récentes (posées par la page de salon) ========== */
(function showRecent() {
  let recent = [];
  try { recent = JSON.parse(localStorage.getItem('sloubi_recent') || '[]'); } catch {}
  if (!Array.isArray(recent)) return;
  recent = recent.filter((c) => typeof c === 'string' && CHAN_RE.test(c) && c !== 'chante');
  if (!recent.length) return;
  for (const c of recent) {
    const a = document.createElement('a');
    a.href = `/${c}`;
    a.textContent = `/${c}`;
    recentBox.appendChild(a);
  }
  recentBox.hidden = false;
})();

/* ========== Démarrage ========== */
const prefill = (document.body.dataset.prefill || '').toLowerCase().slice(0, MAX_LEN);
if (prefill) input.value = prefill; // nom refusé par le serveur : on le montre en rouge
// les polices arrivent après le premier rendu (la pixel seulement au premier rouge) : on remesure
document.fonts?.addEventListener?.('loadingdone', fit);
['1em "Uncial Antiqua"', '1em "Press Start 2P"', '1em MedievalSharp'].forEach((f) => document.fonts?.load(f).then(fit, () => {}));
input.focus();
validate();
