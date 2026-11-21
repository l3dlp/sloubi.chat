/* cyber-fx.js — le cyberpunk de 1984, cousin du punk : un mur de collages
   photocopiés (papiers déchirés, scotch, trame de journal, mots découpés), un
   soleil rayé et une grille fluo au loin, des bandes VHS qui décrochent. Ça
   bouge en « stop motion », comme une animation faite à la photocopieuse.
   Chaque message colle un de ses mots sur le mur ; les pseudos deviennent des
   lettres anonymes ; tous les dix messages, le firmware « se met à jour ». */

import { stage2d, rand, pick, reducedMotion, small, messageText } from '/assets/fx2d.js';

const INK = '#0d0614';
const PALETTE = ['#ff2bd6', '#00f0ff', '#f7ff00', '#f2efe6', '#ff5a1f', '#b8b8b0'];
const WORDS = ['SLOUBI', 'DIY', '1984', 'CTRL+V', 'RÉSISTE', 'PAS DE MAÎTRE', 'COPIE', 'FAIS-LE', 'HACK', 'BRUIT', 'LIBRE', 'NOUS'];

/* ========== Textures faites à la main : grain de photocopie, trame, lignes de balayage ========== */
function tile(size, draw) {
  const c = document.createElement('canvas');
  c.width = c.height = size;
  draw(c.getContext('2d'), size);
  return c;
}

const grain = tile(256, (g, s) => {
  const img = g.createImageData(s, s);
  for (let i = 0; i < img.data.length; i += 4) {
    const v = Math.random();
    const dark = v < 0.07, light = v > 0.97;
    img.data[i] = img.data[i + 1] = img.data[i + 2] = dark ? 0 : 255;
    img.data[i + 3] = dark ? 70 : light ? 50 : 0;
  }
  g.putImageData(img, 0, 0);
});

const dots = tile(8, (g) => { // la trame du papier journal
  g.fillStyle = 'rgba(0,0,0,0.55)';
  g.beginPath();
  g.arc(4, 4, 1.7, 0, Math.PI * 2);
  g.fill();
});

const scan = tile(4, (g) => {
  g.fillStyle = 'rgba(0,0,0,0.22)';
  g.fillRect(0, 0, 4, 1);
});

/* ========== Les morceaux collés au mur ========== */
// un bord déchiré : des points irréguliers le long d'un rectangle
function tornEdge(w, h) {
  const pts = [];
  const side = (x0, y0, x1, y1, n, j) => {
    for (let i = 0; i < n; i++) {
      const k = i / n;
      pts.push([x0 + (x1 - x0) * k + rand(-j, j), y0 + (y1 - y0) * k + rand(-j, j)]);
    }
  };
  const jag = Math.min(w, h) * 0.05 + 1.5;
  side(0, 0, w, 0, 9, jag);
  side(w, 0, w, h, 6, jag);
  side(w, h, 0, h, 9, jag);
  side(0, h, 0, 0, 6, jag);
  return pts;
}

const scraps = [];
const MAX_SCRAPS = small ? 9 : 16;
let W = 0, H = 0;

// on colle dans les marges : le milieu, c'est la conversation, et le haut, l'en-tête
function spot(w) {
  const col = Math.min(680, W - 32);              // la colonne du chat (room.css --col)
  const margin = (W - col) / 2;
  const y = rand(0.13, 0.86) * H;
  if (margin < 90) return { x: Math.random() < 0.5 ? rand(-w * 0.6, -w * 0.2) : rand(W - w * 0.8, W - w * 0.4), y }; // téléphone : à moitié hors champ
  const left = Math.random() < 0.5;
  const x = left ? rand(-w * 0.25, Math.max(-w * 0.2, margin - w - 12)) : rand(W - margin + 12, Math.max(W - margin + 12, W - w * 0.75));
  return { x, y };
}

function addScrap(text, live) {
  const word = (text || pick(WORDS)).toUpperCase().slice(0, 14);
  const big = !text && Math.random() < 0.3;
  const fs = (small ? 15 : 20) * (big ? 1.6 : rand(0.9, 1.25));
  const w = Math.max(70, word.length * fs * 0.72 + 26);
  const h = fs * rand(1.9, 2.6);
  const { x, y } = spot(w);
  scraps.push({
    x, y, w, h, fs, word,
    rot: rand(-0.22, 0.22),
    color: pick(PALETTE),
    news: Math.random() < 0.35,
    tape: Math.random() < 0.6,
    tapeRot: rand(-0.08, 0.08),
    font: pick(['Permanent Marker', 'Archivo', 'Press Start 2P']),
    edge: tornEdge(w, h),
    born: live ? performance.now() : 0,
  });
  while (scraps.length > MAX_SCRAPS) scraps.shift();
}

/* ========== Le dessin ========== */
let glitchUntil = 0;
let vhsAt = 4;

function drawBackdrop(g, t, w, h) {
  g.fillStyle = INK;
  g.fillRect(0, 0, w, h);
  const hy = h * 0.6; // l'horizon

  // le soleil rayé des années 80
  const r = Math.min(w, h) * 0.2;
  const cx = w * 0.5, cy = hy - r * 0.35;
  const sun = g.createLinearGradient(0, cy - r, 0, cy + r);
  sun.addColorStop(0, '#f7ff00');
  sun.addColorStop(0.5, '#ff5a1f');
  sun.addColorStop(1, '#ff2bd6');
  g.save();
  g.beginPath();
  g.arc(cx, cy, r, 0, Math.PI * 2);
  g.clip();
  g.fillStyle = sun;
  g.fillRect(cx - r, cy - r, r * 2, r * 2);
  g.fillStyle = INK;
  for (let i = 0; i < 7; i++) { // les bandes découpées, de plus en plus larges vers le bas
    const yy = cy + r * (0.05 + i * 0.14);
    g.fillRect(cx - r, yy, r * 2, 1.5 + i * 1.6);
  }
  g.restore();
  g.fillStyle = INK;
  g.fillRect(0, hy, w, h - hy);

  // la grille fluo qui défile vers nous
  g.strokeStyle = 'rgba(255, 43, 214, 0.55)';
  g.lineWidth = 1.2;
  g.beginPath();
  const scroll = (t * 0.35) % 1;
  for (let i = 0; i < 14; i++) {
    const k = (i + scroll) / 14;
    const yy = hy + (h - hy) * k * k;
    g.moveTo(0, yy);
    g.lineTo(w, yy);
  }
  for (let i = -12; i <= 12; i++) {
    g.moveTo(cx + i * (w / 26), hy);
    g.lineTo(cx + i * (w / 2.2), h);
  }
  g.stroke();
}

function drawScrap(g, s, t, jitter) {
  const now = performance.now();
  const age = s.born ? (now - s.born) / 1000 : 9;
  // collé à la main : il arrive grand et tourné, puis se plaque au mur
  const slap = age < 0.35 ? 1 + (1 - age / 0.35) * 0.6 : 1;
  g.save();
  g.translate(s.x + jitter.x, s.y + jitter.y);
  g.rotate(s.rot + jitter.r + (age < 0.35 ? (1 - age / 0.35) * 0.4 : 0));
  g.scale(slap, slap);

  g.beginPath();
  s.edge.forEach(([x, y], i) => (i ? g.lineTo(x, y) : g.moveTo(x, y)));
  g.closePath();
  g.fillStyle = 'rgba(0,0,0,0.45)';
  g.save();
  g.translate(4, 5);
  g.fill();
  g.restore();
  g.fillStyle = s.color;
  g.fill();
  if (s.news) {
    g.save();
    g.clip();
    g.fillStyle = g.createPattern(dots, 'repeat');
    g.globalAlpha = 0.35;
    g.fillRect(0, 0, s.w, s.h);
    g.restore();
  }

  g.fillStyle = s.color === '#f2efe6' || s.color === '#f7ff00' || s.color === '#00f0ff' || s.color === '#b8b8b0' ? '#0d0614' : '#f2efe6';
  g.font = `${s.font === 'Press Start 2P' ? s.fs * 0.6 : s.fs}px "${s.font}", sans-serif`;
  g.textBaseline = 'middle';
  g.fillText(s.word, 13, s.h / 2 + 1, s.w - 22);

  if (s.tape) { // un bout de scotch jauni
    g.fillStyle = 'rgba(247, 240, 170, 0.55)';
    g.save();
    g.translate(s.w * 0.5, -3);
    g.rotate(s.tapeRot);
    g.fillRect(-22, -6, 44, 13);
    g.restore();
  }
  g.restore();
}

const stage = stage2d((g, t, dt, w, h) => {
  W = w;
  H = h;
  drawBackdrop(g, t, w, h);

  // « stop motion » : les morceaux tremblent huit fois par seconde, pas plus
  const step = Math.floor(t * 8);
  for (let i = 0; i < scraps.length; i++) {
    const seed = Math.sin((step + 1) * 12.9898 + i * 78.233) * 43758.5453;
    const f = seed - Math.floor(seed);
    drawScrap(g, scraps[i], t, { x: (f - 0.5) * 1.6, y: ((f * 7) % 1 - 0.5) * 1.6, r: (f - 0.5) * 0.006 });
  }

  // le grain de photocopie, décalé à chaque pas
  g.save();
  g.globalAlpha = 0.85;
  g.fillStyle = g.createPattern(grain, 'repeat');
  g.translate((step * 37) % 256, (step * 91) % 256);
  g.fillRect(-256, -256, w + 512, h + 512);
  g.restore();

  // une bande VHS qui décroche de temps en temps, et toutes au moment du « firmware »
  const glitching = t < glitchUntil;
  if (glitching || (t > vhsAt && t < vhsAt + 0.25)) {
    const dpr = stage.dpr;
    for (let i = 0; i < (glitching ? 7 : 2); i++) {
      const y = rand(0, h), bh = rand(6, glitching ? 60 : 24), dx = rand(-30, 30);
      g.drawImage(g.canvas, 0, y * dpr, w * dpr, bh * dpr, dx, y, w, bh);
    }
    if (glitching) { // les couleurs se séparent
      g.save();
      g.globalCompositeOperation = 'screen';
      g.globalAlpha = 0.35;
      g.drawImage(g.canvas, 6, 0, w, h);
      g.restore();
    }
  } else if (t > vhsAt + 0.25) {
    vhsAt = t + rand(5, 11);
  }

  g.fillStyle = g.createPattern(scan, 'repeat');
  g.fillRect(0, 0, w, h);
}, { fps: 24 });

/* ========== Le chat, habillé ========== */
const RANSOM = ['rn-a', 'rn-b', 'rn-c', 'rn-d', 'rn-e'];

// un pseudo en lettres découpées dans des journaux différents
function ransom(el) {
  const nick = el.querySelector('.message-nickname');
  if (!nick || nick.dataset.cut) return;
  nick.dataset.cut = '1';
  const text = nick.textContent;
  nick.textContent = '';
  nick.setAttribute('aria-label', text);
  for (const ch of text) {
    const s = document.createElement('span');
    s.setAttribute('aria-hidden', 'true');
    s.textContent = ch;
    if (ch.trim()) {
      s.className = `rn ${pick(RANSOM)}`;
      s.style.transform = `rotate(${rand(-9, 9).toFixed(1)}deg) translateY(${rand(-2, 2).toFixed(1)}px)`;
    }
    nick.appendChild(s);
  }
}

// le message se « déchiffre » sous tes yeux
const NOISE = '▓▒░#$%&@<>/\\|01';
function decrypt(el) {
  const body = el.querySelector('.message-nickname + span');
  if (!body || reducedMotion) return;
  const text = body.textContent;
  const chars = [...text];
  let i = 0;
  const steps = Math.min(14, 6 + chars.length / 8);
  const timer = setInterval(() => {
    i++;
    const shown = Math.floor((i / steps) * chars.length);
    body.textContent = chars.map((c, k) => (k < shown || c === ' ' ? c : NOISE[Math.floor(Math.random() * NOISE.length)])).join('');
    if (i >= steps) {
      clearInterval(timer);
      body.textContent = text;
    }
  }, 35);
}

// un mot du message, pour le coller au mur
function wordOf(text) {
  const words = (text.match(/[\p{L}\p{N}]{3,12}/gu) || []).sort((a, b) => b.length - a.length);
  return words[0] || '';
}

window.addEventListener('sloubi:message', (e) => {
  const m = e.detail || {};
  if (m.el) ransom(m.el);
  if (!m.live) return;
  if (m.el) decrypt(m.el);
  addScrap(wordOf(m.message || ''), true);
});

window.addEventListener('sloubi:milestone', () => {
  if (reducedMotion) return;
  glitchUntil = (performance.now() - stage.t0) / 1000 + 0.9;
  document.body.classList.remove('glitch');
  void document.body.offsetWidth;
  document.body.classList.add('glitch');
  stage.flareCounter();
});

/* ========== Démarrage : le mur est déjà couvert ========== */
W = window.innerWidth;
H = window.innerHeight;
for (let i = 0; i < MAX_SCRAPS - 3; i++) addScrap('', false);
stage.onResize.push((w, h) => {
  // on recolle tout à la nouvelle taille, pour que le milieu reste lisible
  W = w;
  H = h;
  for (const s of scraps) Object.assign(s, spot(s.w));
});
stage.boot();
