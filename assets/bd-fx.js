/* bd-fx.js — une planche franco-belge en ligne claire : l'écran est une case
   (cadre noir, gouttière blanche), un ciel en trame de points, des nuages
   cernés de noir, une petite ville en aplats. Les messages deviennent des
   bulles, les messages du système des cartouches (« Pendant ce temps… »).
   Une onomatopée dans un message (BOUM, PAF, VLAN…) surgit en grand ; toutes
   les six cases, on tourne la page. */

import { stage2d, rand, pick, reducedMotion, small } from '/assets/fx2d.js';

const INKLINE = '#1a1a1a';
const FLATS = ['#e94f37', '#f2c14e', '#5b8e7d', '#3e6990', '#d98e73', '#f4e1c1'];
let W = window.innerWidth, H = window.innerHeight;

/* ========== La ville, dessinée une fois par taille d'écran ========== */
let town = null;
function drawTown(w, h) {
  const c = document.createElement('canvas');
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  c.width = Math.round(w * dpr);
  c.height = Math.round(h * dpr);
  const g = c.getContext('2d');
  g.scale(dpr, dpr);
  g.lineWidth = 2.4;
  g.strokeStyle = INKLINE;
  g.lineJoin = 'round';
  const base = h - (small ? 70 : 60);
  let x = -10;
  while (x < w + 10) {
    const bw = rand(50, 110) * (small ? 0.75 : 1);
    const bh = rand(70, 200) * (small ? 0.7 : 1);
    const top = base - bh;
    g.fillStyle = pick(FLATS);
    g.beginPath();
    g.rect(x, top, bw, bh + 80);
    g.fill();
    g.stroke();
    // un toit : pointu, plat, ou une coupole
    g.fillStyle = pick(['#a63d40', '#3e6990', '#5b4b49']);
    g.beginPath();
    const kind = Math.random();
    if (kind < 0.5) {
      g.moveTo(x - 4, top);
      g.lineTo(x + bw / 2, top - rand(20, 45));
      g.lineTo(x + bw + 4, top);
    } else if (kind < 0.75) {
      g.rect(x - 4, top - 8, bw + 8, 8);
    } else {
      g.arc(x + bw / 2, top, bw * 0.3, Math.PI, 0);
    }
    g.closePath();
    g.fill();
    g.stroke();
    // des fenêtres, toutes alignées : la ligne claire ne laisse rien au hasard
    g.fillStyle = '#fdf6e3';
    for (let wy = top + 14; wy < base - 18; wy += 26) {
      for (let wx = x + 10; wx < x + bw - 16; wx += 20) {
        g.beginPath();
        g.rect(wx, wy, 9, 13);
        g.fill();
        g.stroke();
      }
    }
    x += bw + rand(-6, 6);
  }
  g.fillStyle = '#c9b79c';
  g.beginPath();
  g.rect(-5, base, w + 10, 200);
  g.fill();
  g.stroke();
  return c;
}

/* ========== Les nuages cernés ========== */
const clouds = Array.from({ length: small ? 3 : 5 }, () => ({ x: rand(0, W), y: rand(0.1, 0.38), s: rand(0.7, 1.2), v: rand(5, 12) }));

function drawCloud(g, c) {
  const x = c.x, y = c.y * H, s = c.s * (small ? 0.75 : 1);
  g.beginPath();
  for (const [dx, dy, r] of [[-30, 4, 18], [-8, -10, 24], [18, -4, 20], [36, 6, 15]]) {
    g.moveTo(x + dx * s + r * s, y + dy * s);
    g.arc(x + dx * s, y + dy * s, r * s, 0, Math.PI * 2);
  }
  g.fillStyle = '#ffffff';
  g.fill();
  g.lineWidth = 2.4;
  g.strokeStyle = INKLINE;
  g.stroke();
  g.fill(); // repeint l'intérieur : seul le contour extérieur reste
}

const stage = stage2d((g, t, dt, w, h) => {
  if (w !== W || h !== H || !town) {
    W = w;
    H = h;
    town = drawTown(w, h);
  }
  g.fillStyle = '#fdf6e3';
  g.fillRect(0, 0, w, h);

  // le ciel : une trame de points qui s'éclaircit vers l'horizon (comme à l'impression)
  g.fillStyle = '#8fd3f4';
  g.fillRect(0, 0, w, h * 0.75);
  g.fillStyle = '#5fb7e5';
  const step = 9;
  for (let y = 0; y < h * 0.62; y += step) {
    const r = 2.9 * (1 - y / (h * 0.62));
    if (r < 0.4) continue;
    for (let x = (y / step) % 2 ? step / 2 : 0; x < w; x += step) {
      g.beginPath();
      g.arc(x, y, r, 0, Math.PI * 2);
      g.fill();
    }
  }

  // le soleil, un aplat jaune et son contour
  g.fillStyle = '#f2c14e';
  g.strokeStyle = INKLINE;
  g.lineWidth = 2.4;
  g.beginPath();
  g.arc(w * 0.82, h * 0.16, Math.min(w, h) * 0.06, 0, Math.PI * 2);
  g.fill();
  g.stroke();

  for (const c of clouds) {
    c.x += c.v * dt;
    if (c.x > w + 80) c.x = -80;
    drawCloud(g, c);
  }

  g.drawImage(town, 0, 0, w, h);

  // l'écran est une case : gouttière blanche, cadre noir
  const m = small ? 6 : 10;
  g.lineWidth = small ? 7 : 10;
  g.strokeStyle = '#ffffff';
  g.strokeRect(0, 0, w, h);
  g.lineWidth = 3;
  g.strokeStyle = INKLINE;
  g.strokeRect(m, m, w - m * 2, h - m * 2);
}, { fps: 30 });

/* ========== Bulles et onomatopées ========== */
// chacun parle toujours du même côté de la case
function side(nick) {
  let h = 0;
  for (const ch of nick || '') h = (h * 31 + ch.codePointAt(0)) >>> 0;
  return h % 2 ? 'bd-r' : 'bd-l';
}

const ONOMATO = /\b(boum|bam|bim|paf|pif|pouf|vlan|crac|crack|splash|bang|zip|wizz|tchac|aïe|ouille|ouch|plouf|clac|vroum|smack|hop|youpi)\b/iu;

function onomatopee(word) {
  const box = document.createElement('div');
  box.className = 'bd-onomato';
  box.setAttribute('aria-hidden', 'true');
  // une étoile d'explosion en SVG, construite à la main (pas d'innerHTML)
  const svgNS = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(svgNS, 'svg');
  svg.setAttribute('viewBox', '-100 -100 200 200');
  const poly = document.createElementNS(svgNS, 'polygon');
  const pts = [];
  const n = 14;
  for (let i = 0; i < n * 2; i++) {
    const a = (i / (n * 2)) * Math.PI * 2;
    const r = i % 2 ? rand(48, 60) : rand(86, 98);
    pts.push(`${(Math.cos(a) * r).toFixed(1)},${(Math.sin(a) * r * 0.7).toFixed(1)}`);
  }
  poly.setAttribute('points', pts.join(' '));
  svg.appendChild(poly);
  const txt = document.createElement('span');
  txt.textContent = `${word.toUpperCase()} !`;
  box.append(svg, txt);
  box.style.left = `${rand(0.18, 0.62) * W}px`;
  box.style.top = `${rand(0.18, 0.42) * H}px`;
  box.style.setProperty('--tilt', `${rand(-14, 14).toFixed(1)}deg`);
  document.body.appendChild(box);
  setTimeout(() => box.remove(), 1900);
}

window.addEventListener('sloubi:message', (e) => {
  const m = e.detail || {};
  if (m.el) m.el.classList.add(side(m.nickname));
  if (!m.live || reducedMotion) return;
  const hit = (m.message || '').match(ONOMATO);
  if (hit) onomatopee(hit[1]);
});

// fin de planche : on tourne la page
window.addEventListener('sloubi:milestone', () => {
  stage.flareCounter();
  if (reducedMotion) return;
  const page = document.createElement('div');
  page.className = 'bd-turn';
  page.setAttribute('aria-hidden', 'true');
  document.body.appendChild(page);
  setTimeout(() => page.remove(), 1300);
});

stage.boot();
