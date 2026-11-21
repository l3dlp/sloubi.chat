/* home.js — l'accueil : on tape le nom d'un salon, on y entre. Le serveur fait
   foi (/api/check) ; ici on réagit vite et on dit pourquoi un nom est refusé.
   Un salon gardé demande son mot de passe ici même : on arrive déverrouillé. */

'use strict';

const CHAN_RE = /^[a-z0-9]+([._-][a-z0-9]+)*$/;
const MAX_LEN = 32;

const form = document.getElementById('prompt');
const input = document.getElementById('chanInput');
const hint = document.getElementById('chanHint');
const recentBox = document.getElementById('recent');
const keyBox = document.getElementById('homeKey');
const keyInput = document.getElementById('homePass');
const keyName = document.getElementById('homeKeyName');
const keyErr = document.getElementById('homeKeyErr');

const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
const cache = new Map(); // nom → réponse de /api/check
let state = 'empty';
let pending = null;      // AbortController de la requête en cours
let debounce = null;

const PLACES = { taverne: 'la taverne', foret: 'la forêt', moulin: 'le moulin', cyber: 'le mur cyberpunk', kawaii: 'le ciel kawaii', bd: 'la planche' };
const HINTS = {
  empty: 'Lettres, chiffres, et un seul . _ - à la fois.',
  wait: '…',
  checking: 'On regarde si ce nom est pris…',
  start: 'Commence par une lettre ou un chiffre.',
  double: 'Un seul séparateur à la fois.',
  chars: 'Lettres, chiffres, . _ - seulement.',
  toolong: '32 caractères au plus.',
  forbidden: 'Ce nom est réservé.',
  slow: 'Trop d’essais d’un coup. Reprends dans un moment.',
  offline: 'Le serveur ne répond pas. Réessaie.',
};

function setState(next, text) {
  state = next;
  document.body.dataset.state = next;
  hint.textContent = text ?? HINTS[next] ?? '';
}

function shake() {
  if (reducedMotion) return;
  form.classList.remove('shake');
  void form.offsetWidth; // relance l'animation
  form.classList.add('shake');
}

/* ========== Validation ========== */
// règles locales : immédiates, les mêmes que le serveur
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
  if (!res.ok) {
    setState('bad', HINTS[res.reason === 'forbidden' ? 'forbidden' : res.reason === 'toolong' ? 'toolong' : 'chars']);
    return;
  }
  if (!res.exists) { setState('ok', 'Libre. Entrée ↵ pour le fonder.'); return; }
  const place = PLACES[res.theme] || 'le salon';
  setState('ok', res.locked && !res.access ? `Existe, gardé par un mot de passe. Entrée ↵ pour rejoindre ${place}.`
    : `Existe. Entrée ↵ pour rejoindre ${place}.`);
}

async function remoteCheck(v) {
  if (cache.has(v)) { applyCheck(cache.get(v)); return; }
  pending?.abort();
  pending = new AbortController();
  const mine = pending;
  setState('wait', HINTS.checking);
  let res;
  try {
    const r = await fetch(`/api/check/${encodeURIComponent(v)}`, { signal: mine.signal, cache: 'no-store' });
    if (r.status === 429) { setState('wait', HINTS.slow); return; }
    if (!r.ok) throw new Error(String(r.status));
    res = await r.json();
  } catch (e) {
    // seulement le réseau ou le serveur : une erreur d'affichage ne doit pas passer pour une panne
    if (e.name !== 'AbortError' && input.value === v) setState('wait', HINTS.offline);
    return;
  }
  cache.set(v, res);
  if (input.value === v) applyCheck(res);
}

function validate() {
  const v = input.value;
  clearTimeout(debounce);
  pending?.abort();
  const verdict = localVerdict(v);
  if (verdict.state !== 'check') { setState(verdict.state, HINTS[verdict.hint || verdict.state]); return; }
  if (cache.has(v)) { applyCheck(cache.get(v)); return; }
  setState('wait');
  debounce = setTimeout(() => remoteCheck(v), 300);
}

input.addEventListener('input', () => {
  // minuscules, et on ignore tout ce qui n'est pas a-z 0-9 . _ -
  const cleaned = input.value.toLowerCase().replace(/[^a-z0-9._-]/g, '').slice(0, MAX_LEN);
  if (cleaned !== input.value) {
    input.value = cleaned;
    input.setSelectionRange(cleaned.length, cleaned.length);
  }
  closeKey(); // on change de salon : la serrure n'est plus la même
  validate();
});

/* ========== Entrer (et, pour un salon gardé, donner le mot de passe ici) ========== */
function closeKey() {
  keyBox.hidden = true;
  keyInput.value = '';
  keyErr.textContent = '';
}

async function enter(name) {
  const res = cache.get(name);
  if (!res || !res.locked || res.access) { location.href = `/${name}`; return; }
  if (keyBox.hidden) {
    keyName.textContent = name;
    keyBox.hidden = false;
    keyInput.focus();
    return;
  }
  if (!keyInput.value) { keyInput.focus(); return; }
  keyErr.textContent = '';
  try {
    const r = await fetch(`/api/${encodeURIComponent(name)}/unlock`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ password: keyInput.value }),
    });
    if (r.ok) { location.href = `/${name}`; return; }
    keyErr.textContent = r.status === 429 ? 'Trop d’essais. Reviens dans un moment.' : 'Ce n’est pas le bon mot de passe.';
  } catch {
    keyErr.textContent = 'Le serveur ne répond pas. Réessaie.';
  }
  shake();
  keyInput.select();
}

function go() {
  if (state === 'ok') { enter(input.value); return; }
  if (state === 'wait' && localVerdict(input.value).state === 'check') {
    // Entrée avant la fin du debounce : on vérifie tout de suite
    remoteCheck(input.value).then(() => { if (state === 'ok') enter(input.value); });
    return;
  }
  shake();
  input.focus();
}

// deux champs, et la flèche : on gère Entrée nous-mêmes depuis l'un ou l'autre
for (const el of [input, keyInput]) {
  el.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' || e.isComposing) return;
    e.preventDefault();
    go();
  });
}
form.addEventListener('submit', (e) => { e.preventDefault(); go(); });

/* ========== Tes salons (posés par la page de salon) ========== */
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
if (!matchMedia('(pointer: coarse)').matches) input.focus(); // sur mobile, pas de clavier qui surgit à l'arrivée
validate();
