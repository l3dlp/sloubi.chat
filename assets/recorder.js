/* recorder.js — l'enregistreur maison : un message vocal, une vidéo, ou son
   écran commenté au micro (sans caméra), sans quitter le salon. Le navigateur compresse pendant l'enregistrement
   (MediaRecorder) : la voix en Opus à 32 kbit/s, la vidéo en 720p à
   1,2 Mbit/s. On réécoute, on revoit, puis l'enregistrement part comme
   n'importe quel fichier (library.js), et le serveur le range tout seul. */

'use strict';

import { Camera, mountControls } from '/assets/camfx.js';

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
  cancel: document.getElementById('recCancel'),
};

const LIMITS = { audio: 15 * 60, video: 5 * 60, screen: 10 * 60 }; // secondes
const TITLES = { audio: 'Message vocal', video: 'Vidéo', screen: 'Mon écran' };
const visual = () => kind !== 'audio'; // une image à montrer : la caméra ou l'écran
const canShare = !!navigator.mediaDevices?.getDisplayMedia && !matchMedia('(pointer: coarse)').matches;
let display = null; // le partage d'écran (ses pistes s'arrêtent avec le reste)

let kind = 'audio';
let stream = null;
let recorder = null;
let chunks = [];
let blob = null;
let mime = '';
let started = 0;
let tick = null;
let cam = null;      // la caméra (camfx.js) : changer de caméra, régler son image
let camTools = null; // ses boutons, sur l'aperçu
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
// les navigateurs de téléphone ne savent pas partager l'écran : l'option n'apparaît que sur ordinateur
const screenItem = menu.querySelector('[data-rec="screen"]');
if (screenItem) screenItem.hidden = !canShare;
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
  return `${k === 'screen' ? 'Écran' : 'Vidéo'} ${stamp}.${mp4 ? 'mp4' : 'webm'}`;
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
  title.textContent = TITLES[k];
  box.dataset.kind = k;
  box.hidden = false;
  setState('asking');
  await getStream();
}

async function getStream() {
  stopStream();
  if (kind === 'screen') { await getScreen(); return; }
  try {
    if (kind === 'audio') {
      stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
    } else {
      // « stable » : l'image passe toujours par le canvas, la piste enregistrée ne change donc
      // jamais, même si l'on change de caméra ou de réglage en pleine prise
      cam = new Camera({
        constraints: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30, max: 30 } },
        stable: true,
        onTrack: (t) => { // seulement sans canvas (vieux navigateur) : on suit la nouvelle piste hors enregistrement
          if (!stream) return;
          for (const v of stream.getVideoTracks()) stream.removeTrack(v);
          stream.addTrack(t);
          live.srcObject = stream;
        },
      });
      const video = await cam.open();
      const mic = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true } });
      stream = new MediaStream([video, ...mic.getAudioTracks()]);
    }
  } catch (e) {
    cam?.stop();
    cam = null;
    setState('error', e?.name === 'NotAllowedError'
      ? `Il faut autoriser ${kind === 'audio' ? 'le micro' : 'la caméra et le micro'} dans le navigateur.`
      : `Impossible d’ouvrir ${kind === 'audio' ? 'le micro' : 'la caméra'}.`);
    return;
  }
  if (kind === 'video') {
    live.srcObject = stream;
    live.classList.toggle('mirror', cam.mirror); // son propre visage, comme dans un miroir
    live.play().catch(() => {});
    camTools = mountControls(document.getElementById('recStage'), cam, { onSwitch: () => live.classList.toggle('mirror', cam.mirror) });
  } else {
    startMeter();
  }
  setState('ready');
}

// l'écran (onglet, fenêtre ou écran entier) et le micro par-dessus, pour commenter
async function getScreen() {
  try {
    display = await navigator.mediaDevices.getDisplayMedia({
      video: { width: { ideal: 1920 }, height: { ideal: 1080 }, frameRate: { ideal: 15, max: 30 } },
      audio: false,
    });
  } catch (e) {
    setState('error', e?.name === 'NotAllowedError' ? 'Partage annulé. Tu peux réessayer avec « Recommencer ».' : 'Impossible de partager l’écran.');
    btn.retry.hidden = false;
    return;
  }
  const video = display.getVideoTracks()[0];
  video.contentHint = 'detail'; // du texte à lire : on garde la netteté, quitte à perdre des images
  // le bouton « Arrêter le partage » du navigateur : on arrête l'enregistrement, ou on referme
  video.addEventListener('ended', () => {
    if (recorder && recorder.state !== 'inactive') btn.stop.click();
    else if (box.dataset.state === 'ready') close();
  });
  let micNote = '';
  let mic = null;
  try {
    mic = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
  } catch {
    micNote = 'Sans micro : l’écran sera enregistré sans le son.';
  }
  stream = new MediaStream([video, ...(mic ? mic.getAudioTracks() : [])]);
  live.srcObject = stream;
  live.classList.remove('mirror');
  live.play().catch(() => {});
  setState('ready', micNote ? `${micNote} Appuie sur « Enregistrer ».` : '');
}

function stopStream() {
  camTools?.remove();
  camTools = null;
  cam?.stop();
  cam = null;
  stream?.getTracks().forEach((t) => t.stop());
  display?.getTracks().forEach((t) => t.stop());
  display = null;
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
  live.hidden = !visual() || done || s === 'error';
  meter.hidden = kind !== 'audio' || done || s === 'error';
  review.hidden = !(done && visual());
  listen.hidden = !(done && kind === 'audio');
  btn.start.hidden = s !== 'ready';
  btn.stop.hidden = !recording;
  btn.send.hidden = !done;
  btn.retry.hidden = !done;
  camTools?.lockSwitch(recording && !cam?.canFilter);
  camTools?.show(!done && s !== 'error'); // à la relecture, on regarde le résultat, pas la caméra
  btn.cancel.textContent = done ? 'Jeter' : 'Annuler';
  note.textContent = message || {
    asking: { audio: 'On ouvre le micro…', video: 'On ouvre la caméra…', screen: 'Choisis ce que tu veux montrer : un onglet, une fenêtre ou tout l’écran.' }[kind],
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
    : kind === 'screen'
      ? { videoBitsPerSecond: 1500000, audioBitsPerSecond: 64000 } // un écran net à 15 images/s, ~11 Mo par minute
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
  if (visual()) review.src = url; else listen.src = url;
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
