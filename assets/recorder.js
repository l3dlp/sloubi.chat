/* recorder.js — l'enregistreur maison : un message vocal ou une vidéo, sans
   quitter le salon. Le navigateur compresse pendant l'enregistrement
   (MediaRecorder) : la voix en Opus à 32 kbit/s, la vidéo en 720p à
   1,2 Mbit/s. On réécoute, on revoit, puis l'enregistrement part comme
   n'importe quel fichier (library.js), et le serveur le range tout seul. */

'use strict';

const button = document.getElementById('recordButton');
const menu = document.getElementById('recordMenu');
const box = document.getElementById('recorder');
const title = document.getElementById('recTitle');
const live = document.getElementById('recLive');
const meter = document.getElementById('recMeter');
const review = document.getElementById('recReview');
const listen = document.getElementById('recListen');
const timeEl = document.getElementById('recTime');
const note = document.getElementById('recNote');
const btn = {
  start: document.getElementById('recStart'),
  stop: document.getElementById('recStop'),
  send: document.getElementById('recSend'),
  retry: document.getElementById('recRetry'),
  flip: document.getElementById('recFlip'),
  cancel: document.getElementById('recCancel'),
};

const LIMITS = { audio: 15 * 60, video: 5 * 60 }; // secondes

let kind = 'audio';
let stream = null;
let recorder = null;
let chunks = [];
let blob = null;
let mime = '';
let started = 0;
let tick = null;
let facing = 'user';
let actx = null;
let raf = 0;
let opener = null;

/* ========== Le menu « + » ========== */
function closeMenu() {
  menu.hidden = true;
  button.setAttribute('aria-expanded', 'false');
}

button.addEventListener('click', () => {
  menu.hidden = !menu.hidden;
  button.setAttribute('aria-expanded', String(!menu.hidden));
  if (!menu.hidden) menu.querySelector('button')?.focus();
});
menu.addEventListener('click', (e) => {
  const k = e.target.closest('[data-rec]')?.dataset.rec;
  if (!k) return;
  closeMenu();
  open(k);
});
menu.addEventListener('keydown', (e) => { if (e.key === 'Escape') { closeMenu(); button.focus(); } });
document.addEventListener('click', (e) => { if (!e.target.closest('.rec-wrap')) closeMenu(); });

/* ========== Le format : ce que le navigateur sait compresser ========== */
// Safari enregistre en MP4, Chrome et Firefox en WebM : on prend le premier qui marche
function pickMime(k) {
  const list = k === 'audio'
    ? ['audio/webm;codecs=opus', 'audio/mp4', 'audio/ogg;codecs=opus', 'audio/webm']
    : ['video/mp4;codecs=avc1,mp4a', 'video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/mp4', 'video/webm'];
  return list.find((t) => window.MediaRecorder?.isTypeSupported?.(t)) || '';
}

// l'extension dit au serveur ce que c'est : .weba / .m4a pour un son, même dans un conteneur vidéo
function fileName(k, type) {
  const d = new Date();
  const stamp = `${d.toLocaleDateString('fr-FR', { day: '2-digit', month: '2-digit' }).replace('/', '-')} ${String(d.getHours()).padStart(2, '0')}h${String(d.getMinutes()).padStart(2, '0')}`;
  const mp4 = type.startsWith('audio/mp4') || type.startsWith('video/mp4');
  if (k === 'audio') return `Message vocal ${stamp}.${mp4 ? 'm4a' : type.startsWith('audio/ogg') ? 'ogg' : 'weba'}`;
  return `Vidéo ${stamp}.${mp4 ? 'mp4' : 'webm'}`;
}

/* ========== Ouvrir, fermer ========== */
async function open(k) {
  if (!window.MediaRecorder || !navigator.mediaDevices?.getUserMedia) {
    alert('Ce navigateur ne sait pas enregistrer. Essaie avec un navigateur à jour.');
    return;
  }
  if (!localStorage.getItem('sloubi_nickname')) {
    window.dispatchEvent(new CustomEvent('sloubi:need-nick'));
    return;
  }
  opener = document.activeElement;
  kind = k;
  title.textContent = k === 'audio' ? 'Message vocal' : 'Vidéo';
  box.hidden = false;
  btn.flip.hidden = k !== 'video' || !matchMedia('(pointer: coarse)').matches; // retourner : sur téléphone
  setState('asking');
  await getStream();
}

async function getStream() {
  stopStream();
  try {
    stream = await navigator.mediaDevices.getUserMedia(kind === 'audio'
      ? { audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } }
      : {
        audio: { echoCancellation: true, noiseSuppression: true },
        video: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30, max: 30 }, facingMode: facing },
      });
  } catch (e) {
    setState('error', e?.name === 'NotAllowedError'
      ? `Il faut autoriser ${kind === 'audio' ? 'le micro' : 'la caméra et le micro'} dans le navigateur.`
      : `Impossible d’ouvrir ${kind === 'audio' ? 'le micro' : 'la caméra'}.`);
    return;
  }
  if (kind === 'video') {
    live.srcObject = stream;
    live.classList.toggle('mirror', facing === 'user'); // son propre visage, comme dans un miroir
    live.play().catch(() => {});
  } else {
    startMeter();
  }
  setState('ready');
}

function stopStream() {
  stream?.getTracks().forEach((t) => t.stop());
  stream = null;
  live.srcObject = null;
  cancelAnimationFrame(raf);
  actx?.close().catch(() => {});
  actx = null;
}

function close() {
  if (recorder && recorder.state !== 'inactive') {
    recorder.onstop = null;
    recorder.stop();
  }
  recorder = null;
  clearInterval(tick);
  stopStream();
  dropBlob();
  box.hidden = true;
  opener?.focus();
}

function dropBlob() {
  if (review.src) URL.revokeObjectURL(review.src);
  if (listen.src) URL.revokeObjectURL(listen.src);
  review.removeAttribute('src');
  listen.removeAttribute('src');
  blob = null;
}

/* ========== Les états de la fenêtre ========== */
function setState(s, message = '') {
  box.dataset.state = s;
  const recording = s === 'recording';
  const done = s === 'review';
  live.hidden = kind !== 'video' || done || s === 'error';
  meter.hidden = kind !== 'audio' || done || s === 'error';
  review.hidden = !(done && kind === 'video');
  listen.hidden = !(done && kind === 'audio');
  btn.start.hidden = s !== 'ready';
  btn.stop.hidden = !recording;
  btn.send.hidden = !done;
  btn.retry.hidden = !done;
  btn.flip.hidden = kind !== 'video' || !matchMedia('(pointer: coarse)').matches || recording || done;
  btn.cancel.textContent = done ? 'Jeter' : 'Annuler';
  note.textContent = message || {
    asking: kind === 'audio' ? 'On ouvre le micro…' : 'On ouvre la caméra…',
    ready: `Appuie sur « Enregistrer ». ${Math.round(LIMITS[kind] / 60)} minutes au plus.`,
    recording: 'Ça enregistre. « Arrêter » quand tu as fini.',
    review: kind === 'audio' ? 'Réécoute, puis envoie (ou recommence).' : 'Revois, puis envoie (ou recommence).',
  }[s] || '';
  if (s === 'ready') { timeEl.textContent = '0:00'; btn.start.focus(); }
  if (s === 'recording') btn.stop.focus();
  if (s === 'review') btn.send.focus();
}

const clock = (sec) => `${Math.floor(sec / 60)}:${String(Math.floor(sec % 60)).padStart(2, '0')}`;

/* ========== Enregistrer ========== */
btn.start.addEventListener('click', () => {
  if (!stream) return;
  mime = pickMime(kind);
  const opts = kind === 'audio'
    ? { audioBitsPerSecond: 32000 } // une voix claire, ~240 Ko par minute
    : { videoBitsPerSecond: 1200000, audioBitsPerSecond: 64000 }; // 720p net, ~9 Mo par minute
  if (mime) opts.mimeType = mime;
  try {
    recorder = new MediaRecorder(stream, opts);
  } catch {
    recorder = new MediaRecorder(stream); // le navigateur choisit lui-même
  }
  chunks = [];
  recorder.ondataavailable = (e) => { if (e.data && e.data.size) chunks.push(e.data); };
  recorder.onstop = finish;
  recorder.start(1000); // des morceaux d'une seconde : rien ne se perd si ça coupe
  started = performance.now();
  tick = setInterval(() => {
    const sec = (performance.now() - started) / 1000;
    const left = LIMITS[kind] - sec;
    timeEl.textContent = left < 30 ? `${clock(sec)} · encore ${Math.ceil(left)} s` : clock(sec);
    if (left <= 0) btn.stop.click(); // la limite : on arrête tout seul
  }, 250);
  setState('recording');
});

btn.stop.addEventListener('click', () => {
  clearInterval(tick);
  if (recorder && recorder.state !== 'inactive') recorder.stop();
});

function finish() {
  const type = recorder?.mimeType || mime || (kind === 'audio' ? 'audio/webm' : 'video/webm');
  blob = new Blob(chunks, { type });
  chunks = [];
  stopStream(); // le micro et la caméra s'éteignent dès qu'on a fini
  const url = URL.createObjectURL(blob);
  if (kind === 'video') review.src = url; else listen.src = url;
  timeEl.textContent = `${clock((performance.now() - started) / 1000)} · ${humanSize(blob.size)}`;
  setState('review');
}

btn.retry.addEventListener('click', async () => {
  dropBlob();
  setState('asking');
  await getStream();
});

btn.send.addEventListener('click', () => {
  if (!blob || !window.sloubiUpload) return;
  const file = new File([blob], fileName(kind, blob.type), { type: blob.type });
  window.sloubiUpload([file]);
  close();
});

btn.cancel.addEventListener('click', close);
btn.flip.addEventListener('click', async () => {
  facing = facing === 'user' ? 'environment' : 'user';
  await getStream();
});
box.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); });

/* ========== Le vumètre : on voit que le micro entend ========== */
function startMeter() {
  try {
    actx = new (window.AudioContext || window.webkitAudioContext)();
    const an = actx.createAnalyser();
    an.fftSize = 1024;
    actx.createMediaStreamSource(stream).connect(an);
    const data = new Uint8Array(an.fftSize);
    const g = meter.getContext('2d');
    const bars = 48;
    const levels = new Array(bars).fill(0);
    let last = 0;
    const color = getComputedStyle(document.body).getPropertyValue('--gold').trim() || '#f2c46d';
    const draw = (t) => {
      raf = requestAnimationFrame(draw);
      if (t - last < 60) return; // 16 images par seconde suffisent
      last = t;
      an.getByteTimeDomainData(data);
      let peak = 0;
      for (const v of data) peak = Math.max(peak, Math.abs(v - 128));
      levels.shift();
      levels.push(Math.min(1, peak / 64));
      g.clearRect(0, 0, meter.width, meter.height);
      g.fillStyle = color;
      const w = meter.width / bars;
      levels.forEach((l, i) => {
        const h = Math.max(4, l * meter.height * 0.9);
        g.globalAlpha = box.dataset.state === 'recording' ? 1 : 0.45;
        g.fillRect(i * w + 2, (meter.height - h) / 2, w - 4, h);
      });
    };
    raf = requestAnimationFrame(draw); // une information (le micro t'entend), pas un décor : même en mouvement réduit
  } catch { /* pas de vumètre : on enregistre quand même */ }
}

function humanSize(n) {
  return window.sloubiFile?.humanSize ? window.sloubiFile.humanSize(n) : `${Math.round(n / 1024)} Ko`;
}

window.addEventListener('pagehide', () => { if (!box.hidden) close(); });
