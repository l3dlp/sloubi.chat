/* call.js — l'appel de groupe d'un lieu gardé (6 personnes au plus).
   Chacun envoie son micro à chacun (maillage WebRTC). La vidéo passe un par un :
   quand la scène est libre, quelqu'un passe en live (caméra ou écran) et les
   autres le regardent. Chaque connexion négocie d'emblée une place vidéo vide ;
   passer en live, c'est y mettre sa piste (replaceTrack), sans renégocier ni
   couper l'appel (l'astuce de BeamCam). Le serveur ne fait que
   relayer la signalisation par le WebSocket du salon (app.js le tient : on lui
   parle par les événements sloubi:send / sloubi:call). Le son ne passe jamais par
   le serveur, sauf par le relais TURN quand un NAT l'impose, et chiffré de bout
   en bout. Réglages audio et relance ICE repris de BeamCam (MIT). */

'use strict';

import { ring, stop as stopRing, unlock as unlockAudio } from '/assets/ring.js';

const CHAN = document.body.dataset.chan;
const API = `/api/${encodeURIComponent(CHAN)}`;
const MAX = 6;

const box = document.getElementById('call');
const peopleEl = document.getElementById('callPeople');
const note = document.getElementById('callNote');
const joinBtn = document.getElementById('callJoin');
const joinLabel = document.getElementById('callJoinLabel');
const muteBtn = document.getElementById('callMute');
const leaveBtn = document.getElementById('callLeave');
const audioBox = document.getElementById('callAudio');
const liveBtn = document.getElementById('callLive');
const liveLabel = document.getElementById('callLiveLabel');
const liveMenu = document.getElementById('liveMenu');
const liveScreen = document.getElementById('liveScreen');
const stageEl = document.getElementById('stage');
const stageVideo = document.getElementById('stageVideo');
const stageWho = document.getElementById('stageWho');
const stageWait = document.getElementById('stageWait');
const stageFull = document.getElementById('stageFull');
const watchBtn = document.getElementById('callWatch');
const bellBtn = document.getElementById('callBell');
const relayBox = document.getElementById('callRelay');
const relayText = document.getElementById('callRelayText');
const relayScreen = document.getElementById('relayScreen');

let state = 'idle';      // idle | joining | in
let me = '';             // notre identifiant dans l'appel (donné par le serveur)
let local = null;        // notre micro
let iceServers = [];
let muted = false;
let roster = [];         // qui est dans l'appel, vu par le serveur : [{id, nick, muted}]
let liveId = '';         // qui est sur scène (le serveur arbitre)
let myLive = null;       // notre live : { track, kind: 'camera' | 'screen' }
let pendingLive = null;  // notre live, en attendant que le serveur nous donne la scène
let listening = false;   // spectateur : on rejoint sans micro
let relayFrom = '';      // la personne en live nous passe la scène (on n'a pas encore accepté)
let relayTimer = null;
let relayTo = '';        // nous sommes en live et proposons la scène à cette personne
let wsOpenAt = 0;        // pour ne pas sonner sur l'état de l'appel reçu à la connexion
let ringTimer = null;
const peers = new Map(); // id → { pc, audio, offerer, video: transceiver, videoTrack, pending: [candidats] }

const nickname = () => { try { return localStorage.getItem('sloubi_nickname') || ''; } catch { return ''; } };
const hasNick = () => { const n = nickname(); return !!n && n.toLowerCase() !== 'anonyme'; };
const send = (msg) => window.dispatchEvent(new CustomEvent('sloubi:send', { detail: { type: 'call', ...msg } }));
const say = (t) => { note.textContent = t; };

/* ========== Qui parle : un vumètre par voix ========== */
let actx = null;
const meters = new Map(); // id → AnalyserNode
let meterTimer = null;

function meter(id, stream) {
  if (!actx || meters.has(id) || !stream.getAudioTracks().length) return;
  const an = actx.createAnalyser();
  an.fftSize = 512;
  actx.createMediaStreamSource(stream).connect(an); // pas vers la sortie : l'élément <audio> joue le son
  meters.set(id, an);
}

function startMeters() {
  const buf = new Float32Array(512);
  meterTimer = setInterval(() => {
    for (const [id, an] of meters) {
      an.getFloatTimeDomainData(buf);
      let sum = 0;
      for (const v of buf) sum += v * v;
      const loud = Math.sqrt(sum / buf.length) > 0.025 && !(id === me && muted);
      peopleEl.querySelector(`[data-id="${CSS.escape(id)}"]`)?.classList.toggle('speaking', loud);
    }
  }, 120);
}

/* ========== Affichage ========== */
function render() {
  box.dataset.state = state;
  const inCall = state === 'in';
  joinBtn.hidden = inCall;
  watchBtn.hidden = inCall || state === 'joining' || roster.length === 0 || roster.length >= MAX; // regarder un appel en cours
  leaveBtn.hidden = !inCall;
  muteBtn.hidden = !inCall || listening;
  joinBtn.disabled = state === 'joining' || (!inCall && roster.length >= MAX);
  joinLabel.textContent = state === 'joining' ? 'Connexion…'
    : roster.length >= MAX ? 'Appel complet'
    : roster.length ? `Rejoindre l'appel (${roster.length}/${MAX})` : 'Lancer l\'appel';
  liveBtn.hidden = !inCall || listening; // un spectateur suit, il ne monte pas sur scène
  const onStage = roster.find((p) => p.id === liveId);
  liveBtn.disabled = !!pendingLive || (!!liveId && liveId !== me && !relayFrom);
  liveBtn.classList.toggle('is-live', !!myLive);
  liveLabel.textContent = myLive ? 'Arrêter le live' : pendingLive ? 'On monte…'
    : onStage ? `${onStage.nick} est en live` : 'Passer en live';
  if (liveBtn.hidden || liveBtn.disabled) closeLiveMenu();
  muteBtn.setAttribute('aria-pressed', String(muted));
  muteBtn.setAttribute('aria-label', muted ? 'Rallumer le micro' : 'Couper le micro');

  // on ne reconstruit les pastilles que si elles changent : sinon un clic tombe sur un élément remplacé
  const sig = JSON.stringify([roster, me, liveId, relayTo, !!myLive, [...peers].map(([id, x]) => [id, x.pc.connectionState])]);
  if (sig !== lastPeopleSig) {
    lastPeopleSig = sig;
    renderPeople();
  }
  box.classList.toggle('has-people', roster.length > 0);
}

let lastPeopleSig = '';
function renderPeople() {
  peopleEl.replaceChildren(...roster.map((p) => {
    const li = document.createElement('li');
    li.className = 'call-person';
    li.dataset.id = p.id;
    li.classList.toggle('muted', !!p.muted);
    li.classList.toggle('is-me', p.id === me);
    li.classList.toggle('on-stage', p.id === liveId);
    li.classList.toggle('listen', !!p.listen);
    li.classList.toggle('relay-pending', p.id === relayTo);
    // en live, un clic sur quelqu'un qui a un micro lui passe la scène
    if (myLive && p.id !== me && !p.listen) {
      li.classList.add('can-relay');
      li.tabIndex = 0;
      li.setAttribute('role', 'button');
      li.title = `Passer le live à ${p.nick}`;
    }
    const face = document.createElement('span');
    face.className = 'call-face';
    face.textContent = [...(p.nick || '?')][0].toUpperCase();
    face.setAttribute('aria-hidden', 'true');
    const name = document.createElement('span');
    name.className = 'call-name';
    name.textContent = p.id === me ? `${p.nick} (toi)` : p.nick;
    if (p.listen) name.title = 'Sans micro';
    li.append(face, name);
    const pc = peers.get(p.id)?.pc;
    if (pc && pc.connectionState !== 'connected') li.classList.add('linking');
    return li;
  }));
}

// un seul écouteur pour toutes les pastilles (elles sont recréées au fil de l'appel)
function relayTarget(e) {
  const li = e.target.closest('.call-person.can-relay');
  return li && roster.find((p) => p.id === li.dataset.id);
}
peopleEl.addEventListener('click', (e) => { const p = relayTarget(e); if (p) handoff(p); });
peopleEl.addEventListener('keydown', (e) => {
  if (e.key !== 'Enter' && e.key !== ' ') return;
  const p = relayTarget(e);
  if (p) { e.preventDefault(); handoff(p); }
});

/* ========== Une connexion par personne ========== */
function signal(to, data) { send({ op: 'signal', to, data }); }

function createPeer(id, offerer) {
  const pc = new RTCPeerConnection({ iceServers, iceTransportPolicy: 'all' });
  const audio = document.createElement('audio');
  audio.autoplay = true;
  audio.playsInline = true;
  audioBox.appendChild(audio);
  const peer = { pc, audio, offerer, pending: [] };
  peers.set(id, peer);

  if (offerer) {
    for (const t of local.getTracks()) pc.addTrack(t, local);
    if (!local.getAudioTracks().length) pc.addTransceiver('audio', { direction: 'recvonly' }); // spectateur : on écoute seulement
    // la place de la vidéo, vide pour l'instant : le live s'y glissera sans renégocier
    peer.video = pc.addTransceiver('video', { direction: 'sendrecv' });
    if (myLive) sendLive(peer);
    // le nouveau venu fait les offres : jamais deux offres croisées
    pc.onnegotiationneeded = async () => {
      try {
        await pc.setLocalDescription();
        signal(id, { sdp: pc.localDescription });
      } catch (e) { console.warn('offre', e); }
    };
  }
  pc.onicecandidate = (e) => { if (e.candidate) signal(id, { candidate: e.candidate.toJSON() }); };
  pc.ontrack = (e) => {
    if (e.track.kind === 'video') {
      peer.videoTrack = e.track;
      e.track.addEventListener('unmute', updateStage); // les premières images arrivent
      updateStage();
      return;
    }
    const stream = new MediaStream([e.track]);
    audio.srcObject = stream;
    audio.play().catch(() => {});
    meter(id, stream);
  };
  pc.onconnectionstatechange = () => {
    // un réseau qui change (wifi → 4G) : on relance ICE plutôt que de raccrocher
    if (pc.connectionState === 'failed' && offerer) pc.restartIce();
    if (pc.connectionState === 'connected' && myLive) tuneAll(); // les paramètres d'envoi ne prennent qu'une fois connecté
    render();
  };
  return peer;
}

function closePeer(id) {
  const p = peers.get(id);
  if (!p) return;
  p.pc.close();
  p.audio.srcObject = null;
  p.audio.remove();
  peers.delete(id);
  meters.delete(id);
}

async function onSignal(from, data) {
  if (state !== 'in' || !data) return;
  const peer = peers.get(from) || createPeer(from, false);
  const { pc } = peer;
  try {
    if (data.sdp) {
      await pc.setRemoteDescription(data.sdp);
      if (data.sdp.type === 'offer') {
        // on répond avec notre micro, ajouté après l'offre pour réutiliser ses transceivers
        const senders = pc.getSenders().map((s) => s.track);
        for (const t of local.getTracks()) if (!senders.includes(t)) pc.addTrack(t, local);
        // la place vidéo offerte : on la passe en envoi aussi, pour pouvoir passer en live plus tard
        if (!peer.video) {
          peer.video = pc.getTransceivers().find((t) => t.receiver.track?.kind === 'video') || null;
          if (peer.video) peer.video.direction = 'sendrecv';
        }
        await pc.setLocalDescription();
        if (myLive) sendLive(peer);
        signal(from, { sdp: pc.localDescription });
      }
      for (const c of peer.pending.splice(0)) await pc.addIceCandidate(c);
    } else if (data.candidate) {
      if (pc.remoteDescription) await pc.addIceCandidate(data.candidate);
      else peer.pending.push(data.candidate);
    }
  } catch (e) {
    console.warn('signal', e);
  }
}

/* ========== Le live : une personne sur scène, les autres regardent ========== */
const canShare = !!navigator.mediaDevices?.getDisplayMedia && !matchMedia('(pointer: coarse)').matches;
liveScreen.hidden = !canShare; // les navigateurs mobiles ne partagent pas l'écran

// le débit d'un live, selon ce qu'on montre et le nombre de spectateurs (on envoie un flux à chacun)
function tune(peer) {
  const sender = peer.video?.sender;
  if (!sender || !myLive) return;
  const n = Math.max(1, peers.size);
  const p = sender.getParameters();
  if (!p.encodings || !p.encodings.length) return; // pas encore négocié : on réessaiera une fois connecté
  const screen = myLive.kind === 'screen';
  p.encodings[0].maxBitrate = screen ? (n <= 2 ? 1_500_000 : 900_000) : (n <= 2 ? 1_200_000 : n <= 3 ? 850_000 : 600_000);
  // un écran garde sa résolution (le texte reste lisible), une caméra peut la réduire
  p.encodings[0].scaleResolutionDownBy = screen ? 1 : (n <= 3 ? 1 : 1.5);
  p.degradationPreference = screen ? 'maintain-resolution' : 'balanced';
  sender.setParameters(p).catch(() => {});
}
const tuneAll = () => { for (const peer of peers.values()) tune(peer); };

function sendLive(peer) {
  if (!peer.video) return;
  peer.video.sender.replaceTrack(myLive ? myLive.track : null).then(() => tune(peer)).catch(() => {});
}

async function goLive(kind) {
  closeLiveMenu();
  if (state !== 'in' || listening || myLive || pendingLive || (liveId && liveId !== me && !relayFrom)) return;
  hideRelay();
  let track;
  try {
    const stream = kind === 'screen'
      ? await navigator.mediaDevices.getDisplayMedia({ video: { width: { ideal: 1920 }, height: { ideal: 1080 }, frameRate: { ideal: 15, max: 30 } }, audio: false })
      : await navigator.mediaDevices.getUserMedia({ video: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 }, facingMode: 'user' }, audio: false });
    track = stream.getVideoTracks()[0];
  } catch (e) {
    say(e && e.name === 'NotAllowedError' ? (kind === 'screen' ? 'Partage d’écran annulé.' : 'La caméra a été refusée.') : 'Impossible d’ouvrir la vidéo.');
    return;
  }
  track.contentHint = kind === 'screen' ? 'detail' : 'motion';
  // le bouton « Arrêter le partage » du navigateur, ou une caméra débranchée
  track.addEventListener('ended', () => { if (myLive?.track === track || pendingLive?.track === track) stopLive(); });
  pendingLive = { track, kind };
  relayFrom = '';
  send({ op: 'live', on: true });
  render();
}

// le serveur nous a donné la scène : la piste part vers tout le monde
function startLive() {
  myLive = pendingLive;
  pendingLive = null;
  for (const peer of peers.values()) sendLive(peer);
  say('');
}

function dropLive() {
  relayTo = '';
  const t = (myLive || pendingLive)?.track;
  myLive = pendingLive = null;
  t?.stop();
  for (const peer of peers.values()) sendLive(peer);
}

function stopLive() {
  if (!myLive && !pendingLive) return;
  dropLive();
  send({ op: 'live', on: false });
  updateStage();
  render();
}

function updateStage() {
  const show = state === 'in' && !!liveId;
  stageEl.hidden = !show;
  document.body.classList.toggle('on-stage', show);
  if (!show) { stageVideo.srcObject = null; return; }
  const who = roster.find((p) => p.id === liveId);
  let track = null;
  if (liveId === me) {
    track = myLive?.track || null;
    stageWho.textContent = myLive?.kind === 'screen' ? 'Tu partages ton écran' : 'Tu es en live';
  } else {
    track = peers.get(liveId)?.videoTrack || null;
    stageWho.textContent = `${who?.nick || '…'} en live`;
  }
  stageEl.classList.toggle('mirror', liveId === me && myLive?.kind === 'camera'); // son propre visage, comme dans un miroir
  if (track && stageVideo.srcObject?.getVideoTracks()[0] !== track) {
    stageVideo.srcObject = new MediaStream([track]);
    stageVideo.play().catch(() => {});
  }
  stageWait.hidden = !!track && !track.muted;
}

stageVideo.addEventListener('playing', () => { stageWait.hidden = true; });
stageVideo.addEventListener('dblclick', () => toggleFull());
stageFull.addEventListener('click', () => toggleFull());
function toggleFull() {
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
  else (stageEl.requestFullscreen || stageEl.webkitRequestFullscreen)?.call(stageEl);
}

function closeLiveMenu() {
  liveMenu.hidden = true;
  liveBtn.setAttribute('aria-expanded', 'false');
}

liveBtn.addEventListener('click', () => {
  if (myLive || pendingLive) { stopLive(); return; }
  if (!canShare) { goLive('camera'); return; } // sur mobile, seulement la caméra : pas de menu
  liveMenu.hidden = !liveMenu.hidden;
  liveBtn.setAttribute('aria-expanded', String(!liveMenu.hidden));
  if (!liveMenu.hidden) liveMenu.querySelector('button')?.focus();
});
liveMenu.addEventListener('click', (e) => {
  const src = e.target.closest('[data-src]')?.dataset.src;
  if (src) goLive(src);
});
liveMenu.addEventListener('keydown', (e) => { if (e.key === 'Escape') { closeLiveMenu(); liveBtn.focus(); } });
document.addEventListener('click', (e) => { if (!e.target.closest('.live-wrap')) closeLiveMenu(); });

/* ========== Rejoindre, couper, raccrocher ========== */
function askNick() {
  say('Choisis d’abord ton pseudo : tape-le dans le champ en bas.');
  window.dispatchEvent(new CustomEvent('sloubi:need-nick'));
}

async function join(listen = false) {
  if (state !== 'idle') return;
  if (!window.RTCPeerConnection || (!listen && !navigator.mediaDevices?.getUserMedia)) { say('Ce navigateur ne sait pas téléphoner.'); return; }
  if (!hasNick()) { askNick(); return; }
  stopRinging();
  state = 'joining';
  listening = listen;
  say('');
  render();
  try {
    // ce clic est aussi le geste qu'iOS exige avant de jouer du son
    actx = new (window.AudioContext || window.webkitAudioContext)();
    const [stream, cfg] = await Promise.all([
      listen ? Promise.resolve(new MediaStream())
        : navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: false }),
      fetch(`${API}/turn`, { cache: 'no-store' }).then((r) => {
        if (r.status === 403) throw new Error('locked');
        return r.ok ? r.json() : { iceServers: [] };
      }),
    ]);
    local = stream;
    iceServers = cfg.iceServers || [];
    muted = listen;
    send({ op: 'join', nick: nickname(), listen });
    // la réponse « joined » arrive par le WebSocket
  } catch (e) {
    cleanup();
    say(e && e.name === 'NotAllowedError' ? 'Le micro a été refusé. Autorise-le pour parler, ou rejoins « Sans micro ».'
      : e && e.message === 'locked' ? 'Il faut le mot de passe pour appeler.' : 'Impossible d’ouvrir le micro.');
  }
}

function cleanup() {
  dropLive();
  liveId = '';
  for (const id of [...peers.keys()]) closePeer(id);
  local?.getTracks().forEach((t) => t.stop());
  local = null;
  clearInterval(meterTimer);
  meterTimer = null;
  meters.clear();
  actx?.close().catch(() => {});
  actx = null;
  me = '';
  state = 'idle';
  listening = false;
  hideRelay();
  updateStage();
  render();
}

function leave() {
  if (state === 'idle') return;
  send({ op: 'leave' });
  cleanup();
}

function toggleMute() {
  if (!local) return;
  muted = !muted;
  for (const t of local.getAudioTracks()) t.enabled = !muted; // on envoie du silence, la connexion reste
  send({ op: 'state', muted });
  render();
}

joinBtn.addEventListener('click', () => join(false));
watchBtn.addEventListener('click', () => join(true));
leaveBtn.addEventListener('click', leave);
muteBtn.addEventListener('click', toggleMute);

/* ========== Ce que dit le serveur ========== */
window.addEventListener('sloubi:call', (e) => {
  const m = e.detail || {};
  switch (m.op) {
    case 'joined':
      if (state !== 'joining') { send({ op: 'leave' }); return; } // on a renoncé entre-temps
      me = m.you;
      state = 'in';
      meter(me, local);
      startMeters();
      for (const p of m.peers || []) createPeer(p.id, true);
      say((m.peers || []).length ? '' : 'Tu es seul·e pour l’instant. Les autres verront l’appel en cours.');
      render();
      break;
    case 'full':
      cleanup();
      say(`L’appel est complet (${MAX} personnes).`);
      break;
    case 'denied':
      cleanup();
      say('Les appels sont réservés aux lieux gardés par un mot de passe.');
      break;
    case 'nick-required':
      cleanup();
      askNick();
      break;
    case 'handoff':
      showRelay(m.from, m.nick);
      break;
    case 'handoff-declined':
      relayTo = '';
      say(`${m.nick} préfère regarder. Tu gardes la scène.`);
      render();
      break;
    case 'stage-busy':
      dropLive();
      say('Quelqu’un est déjà en live. Attends qu’il ou elle rende la scène.');
      render();
      break;
    case 'signal':
      onSignal(m.from, m.data);
      break;
    case 'roster':
      const wasEmpty = roster.length === 0;
      roster = (m.peers || []).slice(0, MAX);
      liveId = m.live || '';
      if (wasEmpty && roster.length && state === 'idle' && Date.now() - wsOpenAt > 2500) incoming(roster[0].nick);
      if (!roster.length) stopRinging();
      if (relayFrom && liveId !== relayFrom) hideRelay(); // l'offre ne tient plus
      // quelqu'un est parti : on ferme sa connexion
      for (const id of [...peers.keys()]) if (!roster.some((p) => p.id === id)) closePeer(id);
      if (pendingLive && liveId === me) startLive();
      else if (myLive && liveId !== me) dropLive(); // on nous a retiré la scène
      if (myLive) tuneAll(); // le nombre de spectateurs a pu changer
      if (state === 'in' && roster.length > 1 && !pendingLive) say('');
      updateStage();
      render();
      break;
  }
});

// sans WebSocket, plus de signalisation : on raccroche proprement
window.addEventListener('sloubi:ws-open', () => {
  wsOpenAt = Date.now();
  sayHello();
});

window.addEventListener('sloubi:ws-closed', () => {
  roster = [];
  if (state !== 'idle') {
    cleanup();
    say('Connexion perdue : l’appel est coupé. Tu peux le rejoindre à nouveau.');
  } else {
    render();
  }
});

window.addEventListener('pagehide', leave);

/* ========== Passer le relais : la personne en live donne la scène en un clic ========== */
function handoff(p) {
  if (!myLive) return;
  relayTo = p.id;
  send({ op: 'handoff', to: p.id });
  say(`Tu passes le live à ${p.nick}. Dès qu’il ou elle accepte, tu descends de scène.`);
  render();
}

relayScreen.hidden = !canShare;

function showRelay(from, nick) {
  if (state !== 'in' || listening) return;
  relayFrom = from;
  relayText.textContent = `${nick} te passe le live`;
  relayBox.hidden = false;
  relayBox.querySelector('[data-relay]')?.focus();
  ring();
  clearTimeout(relayTimer);
  relayTimer = setTimeout(() => declineRelay(), 30000); // sans réponse, on décline poliment
  render();
}

function hideRelay() {
  clearTimeout(relayTimer);
  relayBox.hidden = true;
  stopRing();
}

function declineRelay() {
  if (!relayFrom) return;
  relayFrom = '';
  hideRelay();
  send({ op: 'handoff-decline' });
  render();
}

relayBox.addEventListener('click', (e) => {
  const what = e.target.closest('[data-relay]')?.dataset.relay;
  if (!what) return;
  if (what === 'no') declineRelay();
  else goLive(what); // la caméra ou l'écran ; le navigateur demande l'autorisation s'il le faut
});
relayBox.addEventListener('keydown', (e) => { if (e.key === 'Escape') declineRelay(); });

/* ========== On sonne : un appel commence ========== */
function incoming(nick) {
  ring(); // ne joue que si la page a déjà reçu un clic (règle des navigateurs) ; le bouton bat quand même
  box.classList.add('ringing');
  clearTimeout(ringTimer);
  ringTimer = setTimeout(stopRinging, 12000);
  say(`${nick} a lancé l’appel.`);
  // onglet caché et notifications acceptées : on prévient aussi par le système
  if (document.hidden && 'Notification' in window && Notification.permission === 'granted') {
    navigator.serviceWorker?.getRegistration('/').then((reg) => reg?.showNotification(`On t’appelle dans /${CHAN}`, {
      body: `${nick} a lancé l’appel. Viens !`, tag: `call-${CHAN}`, renotify: true, icon: '/assets/icon-192.png', data: { url: `/${CHAN}` },
    })).catch(() => {});
  }
}

function stopRinging() {
  clearTimeout(ringTimer);
  box.classList.remove('ringing');
  stopRing();
}

/* ========== Notifications d'appel (Web Push), lieu par lieu ========== */
const PUSH_KEY = `sloubi_push_${CHAN}`;
const pushOk = 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
const ls = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { if (v === null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch {} },
};

// un identifiant par navigateur : le serveur ne pousse pas vers qui est déjà dans le salon
function pushTag() {
  let t = ls.get('sloubi_push_tag');
  if (!t) {
    const b = crypto.getRandomValues(new Uint8Array(12));
    t = btoa(String.fromCharCode(...b)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    ls.set('sloubi_push_tag', t);
  }
  return t;
}

function sayHello() {
  if (ls.get(PUSH_KEY)) window.dispatchEvent(new CustomEvent('sloubi:send', { detail: { type: 'hello', push: pushTag() } }));
}

const b64ToBytes = (s) => {
  const b = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4));
  return Uint8Array.from(b, (c) => c.charCodeAt(0));
};

async function enablePush() {
  const reg = await navigator.serviceWorker.register('/sw.js');
  if (await Notification.requestPermission() !== 'granted') {
    say('Les notifications sont bloquées par le navigateur.');
    return false;
  }
  const r = await fetch('/api/push-key');
  if (!r.ok) { say('Les notifications ne sont pas disponibles ici.'); return false; }
  const { key } = await r.json();
  await navigator.serviceWorker.ready;
  let sub = await reg.pushManager.getSubscription();
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(key) });
  const j = sub.toJSON();
  const res = await fetch(`${API}/push`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ endpoint: j.endpoint, keys: j.keys, tag: pushTag() }),
  });
  if (!res.ok) { say('Le serveur n’a pas accepté l’abonnement.'); return false; }
  ls.set(PUSH_KEY, '1');
  sayHello();
  say('Tu seras prévenu·e quand quelqu’un appelle ici, même onglet fermé.');
  return true;
}

async function disablePush() {
  ls.set(PUSH_KEY, null);
  const sub = await (await navigator.serviceWorker.getRegistration('/'))?.pushManager.getSubscription();
  if (sub) {
    await fetch(`${API}/push`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ endpoint: sub.endpoint, remove: true }),
    }).catch(() => {});
  }
  say('Plus de notifications pour ce lieu.');
}

function renderBell() {
  const on = !!ls.get(PUSH_KEY) && Notification.permission === 'granted';
  bellBtn.setAttribute('aria-pressed', String(on));
  bellBtn.title = on ? 'Ne plus me prévenir des appels' : 'Me prévenir quand quelqu’un appelle';
  bellBtn.setAttribute('aria-label', bellBtn.title);
}

if (pushOk) {
  bellBtn.hidden = false;
  renderBell();
  bellBtn.addEventListener('click', async () => {
    unlockAudio();
    bellBtn.disabled = true;
    try {
      if (bellBtn.getAttribute('aria-pressed') === 'true') await disablePush();
      else await enablePush();
    } catch (e) {
      console.warn('push', e);
      say('Impossible d’activer les notifications ici.');
    } finally {
      bellBtn.disabled = false;
      renderBell();
    }
  });
}

render();
