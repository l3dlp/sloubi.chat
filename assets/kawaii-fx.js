/* kawaii-fx.js — un ciel pastel où tout a des yeux : des nuages qui clignent,
   des mochis qui respirent en bas de l'écran, des étoiles qui scintillent.
   Un message fait sauter un mochi (avec des cœurs) ; tous les dix, un
   arc-en-ciel. Et le bouton envoyer est un petit personnage : il suit ton
   curseur, regarde ce que tu écris, s'endort si tu l'ignores, sourit quand tu
   envoies. */

import { stage2d, rand, pick, reducedMotion, small } from '/assets/fx2d.js';

const PASTELS = ['#ffb3d1', '#c9b6ff', '#a8e6cf', '#ffd59e', '#9fd8ff', '#ffc4b8'];
let W = window.innerWidth, H = window.innerHeight;

/* ========== Les habitants ========== */
function cloud(i) {
  return {
    x: rand(0, W), y: rand(0.08, 0.42) * H, s: rand(0.7, 1.25) * (small ? 0.7 : 1),
    speed: rand(6, 16) * (i % 2 ? 1 : -1), blinkAt: rand(1, 6), face: Math.random() < 0.8,
  };
}
const clouds = Array.from({ length: small ? 3 : 5 }, (_, i) => cloud(i));

const MOCHIS = small ? 4 : 6;
const mochis = Array.from({ length: MOCHIS }, (_, i) => ({
  slot: (i + 0.5) / MOCHIS, color: PASTELS[i % PASTELS.length], r: rand(22, 34) * (small ? 0.8 : 1),
  phase: rand(0, 6), jump: -1, blinkAt: rand(1, 5),
}));

const stars = Array.from({ length: small ? 18 : 34 }, () => ({ x: Math.random(), y: rand(0, 0.7), r: rand(2, 5), phase: rand(0, 6) }));
const hearts = [];
let rainbow = -1; // instant de départ de l'arc-en-ciel

/* ========== Dessin ========== */
function face(g, x, y, s, blink, happy) {
  g.fillStyle = '#4a2f4f';
  for (const dx of [-1, 1]) {
    g.beginPath();
    if (happy) { // ^^
      g.lineWidth = 2.2 * s;
      g.strokeStyle = '#4a2f4f';
      g.arc(x + dx * 7 * s, y + 1 * s, 3.4 * s, Math.PI * 1.1, Math.PI * 1.9);
      g.stroke();
    } else {
      g.ellipse(x + dx * 7 * s, y, 2.6 * s, 3.4 * s * (blink ? 0.15 : 1), 0, 0, Math.PI * 2);
      g.fill();
    }
  }
  g.fillStyle = 'rgba(255, 120, 160, 0.45)'; // les joues
  for (const dx of [-1, 1]) {
    g.beginPath();
    g.ellipse(x + dx * 13 * s, y + 5 * s, 4 * s, 2.4 * s, 0, 0, Math.PI * 2);
    g.fill();
  }
  g.strokeStyle = '#4a2f4f';
  g.lineWidth = 1.8 * s;
  g.beginPath();
  g.arc(x, y + 4 * s, 3 * s, 0.15 * Math.PI, 0.85 * Math.PI);
  g.stroke();
}

function drawCloud(g, c, t) {
  const s = c.s;
  g.fillStyle = 'rgba(255,255,255,0.92)';
  g.beginPath();
  for (const [dx, dy, r] of [[-34, 6, 22], [-12, -10, 28], [16, -6, 26], [38, 8, 20], [0, 12, 26]]) {
    g.moveTo(c.x + dx * s + r * s, c.y + dy * s);
    g.arc(c.x + dx * s, c.y + dy * s, r * s, 0, Math.PI * 2);
  }
  g.fill();
  if (c.face) face(g, c.x, c.y + 4 * s, s * 0.9, (t % 6) - c.blinkAt % 6 > 0 && (t % 6) - c.blinkAt % 6 < 0.13, false);
}

function drawMochi(g, m, t) {
  const groundY = H - (small ? 120 : 96);
  const x = m.slot * W;
  let y = groundY, sx = 1, sy = 1;
  const breathe = Math.sin(t * 2 + m.phase) * 0.04;
  sx += breathe;
  sy -= breathe;
  if (m.jump >= 0) {
    const k = (t - m.jump) / 0.9;
    if (k >= 1) m.jump = -1;
    else {
      y -= Math.sin(Math.PI * k) * 90;
      // écrasé au départ et à l'arrivée, étiré en l'air
      const squash = k < 0.12 || k > 0.88 ? 0.22 : -0.12;
      sx += squash;
      sy -= squash;
    }
  }
  const r = m.r;
  g.save();
  g.translate(x, y);
  g.scale(sx, sy);
  g.fillStyle = 'rgba(74, 47, 79, 0.12)';
  g.beginPath();
  g.ellipse(0, 2, r * 1.05, r * 0.25, 0, 0, Math.PI * 2);
  g.fill();
  g.fillStyle = m.color;
  g.beginPath();
  g.ellipse(0, -r * 0.8, r * 1.15, r * 0.95, 0, 0, Math.PI * 2);
  g.fill();
  g.fillStyle = 'rgba(255,255,255,0.55)';
  g.beginPath();
  g.ellipse(-r * 0.45, -r * 1.25, r * 0.28, r * 0.16, -0.5, 0, Math.PI * 2);
  g.fill();
  const blink = ((t + m.blinkAt) % 4.5) < 0.12;
  face(g, 0, -r * 0.8, r / 26, blink, m.jump >= 0);
  g.restore();
  return { x, y: y - r * 1.6 };
}

function drawHeart(g, x, y, s, a) {
  g.save();
  g.globalAlpha = a;
  g.fillStyle = '#ff7eb6';
  g.translate(x, y);
  g.scale(s, s);
  g.beginPath();
  g.moveTo(0, 4);
  g.bezierCurveTo(-8, -3, -4, -10, 0, -5);
  g.bezierCurveTo(4, -10, 8, -3, 0, 4);
  g.fill();
  g.restore();
}

function drawRainbow(g, t) {
  const k = (t - rainbow) / 4;
  if (rainbow < 0 || k > 1) { rainbow = -1; return; }
  const sweep = Math.min(1, k * 2.2);
  const fade = k < 0.7 ? 1 : 1 - (k - 0.7) / 0.3;
  const cx = W / 2, cy = H * 0.72, R = Math.min(W, H) * 0.62;
  g.save();
  g.globalAlpha = 0.55 * fade;
  g.lineWidth = Math.max(10, R * 0.045);
  PASTELS.slice().reverse().forEach((c, i) => {
    g.strokeStyle = c;
    g.beginPath();
    g.arc(cx, cy, R - i * g.lineWidth, Math.PI, Math.PI + Math.PI * sweep);
    g.stroke();
  });
  g.restore();
}

const stage = stage2d((g, t, dt, w, h) => {
  W = w;
  H = h;
  const sky = g.createLinearGradient(0, 0, 0, h);
  sky.addColorStop(0, '#ffd6ea');
  sky.addColorStop(0.55, '#e8dcff');
  sky.addColorStop(1, '#d4f5ec');
  g.fillStyle = sky;
  g.fillRect(0, 0, w, h);

  // des pois très doux
  g.fillStyle = 'rgba(255,255,255,0.35)';
  for (let y = 18; y < h; y += 44) {
    for (let x = (y / 44) % 2 ? 22 : 0; x < w; x += 44) {
      g.beginPath();
      g.arc(x, y, 3, 0, Math.PI * 2);
      g.fill();
    }
  }

  for (const s of stars) { // des étoiles à quatre branches qui scintillent
    const a = 0.35 + 0.65 * Math.max(0, Math.sin(t * 1.6 + s.phase));
    const x = s.x * w, y = s.y * h, r = s.r * (0.7 + 0.3 * a);
    g.fillStyle = `rgba(255, 255, 255, ${a})`;
    g.beginPath();
    g.moveTo(x, y - r * 2);
    g.quadraticCurveTo(x, y, x + r * 2, y);
    g.quadraticCurveTo(x, y, x, y + r * 2);
    g.quadraticCurveTo(x, y, x - r * 2, y);
    g.quadraticCurveTo(x, y, x, y - r * 2);
    g.fill();
  }

  drawRainbow(g, t);

  for (const c of clouds) {
    c.x += c.speed * dt;
    if (c.x > w + 90) c.x = -90;
    if (c.x < -90) c.x = w + 90;
    drawCloud(g, c, t);
  }

  // le sol, une colline toute ronde
  g.fillStyle = '#c8f0dd';
  g.beginPath();
  g.ellipse(w / 2, h + h * 0.18, w * 0.85, h * 0.32, 0, Math.PI, 0);
  g.fill();

  for (const m of mochis) m.top = drawMochi(g, m, t);

  for (let i = hearts.length - 1; i >= 0; i--) {
    const p = hearts[i];
    p.age += dt;
    if (p.age > 1.6) { hearts.splice(i, 1); continue; }
    if (p.age < 0) continue; // départ décalé : pas encore là
    drawHeart(g, p.x + Math.sin(p.age * 5 + p.phase) * 8, p.y - p.age * 70, p.s, 1 - p.age / 1.6);
  }
}, { fps: 60 });

/* ========== Réactions ========== */
window.addEventListener('sloubi:message', (e) => {
  if (!e.detail?.live || reducedMotion) return;
  const t = (performance.now() - stage.t0) / 1000;
  const m = pick(mochis);
  m.jump = t;
  for (let i = 0; i < 6; i++) {
    hearts.push({ x: m.slot * W + rand(-20, 20), y: H - (small ? 120 : 96) - m.r * 2, s: rand(0.9, 1.6), age: -i * 0.08, phase: rand(0, 6) });
  }
});

window.addEventListener('sloubi:milestone', () => {
  if (reducedMotion) return;
  const t = (performance.now() - stage.t0) / 1000;
  rainbow = t;
  mochis.forEach((m, i) => { m.jump = t + i * 0.12; });
  stage.flareCounter();
});

/* ========== Le bouton envoyer a des yeux ========== */
(function sendBuddy() {
  const btn = document.getElementById('sendButton');
  const input = document.getElementById('chatInput');
  if (!btn || !input) return;
  const buddy = document.createElement('span');
  buddy.className = 'kw-buddy';
  buddy.setAttribute('aria-hidden', 'true');
  for (let i = 0; i < 2; i++) {
    const eye = document.createElement('span');
    eye.className = 'kw-eye';
    eye.appendChild(document.createElement('i'));
    buddy.appendChild(eye);
  }
  const mouth = document.createElement('span');
  mouth.className = 'kw-mouth';
  buddy.appendChild(mouth);
  btn.prepend(buddy); // la tête, puis le mot « Envoyer »
  const pupils = buddy.querySelectorAll('.kw-eye i');

  const look = (dx, dy) => {
    const len = Math.hypot(dx, dy) || 1;
    const k = Math.min(1, len / 120) * 3.2;
    pupils.forEach((p) => { p.style.transform = `translate(${((dx / len) * k).toFixed(1)}px, ${((dy / len) * k).toFixed(1)}px)`; });
  };

  let idle = 0;
  const wake = () => { idle = 0; buddy.classList.remove('sleepy'); };
  setInterval(() => { if (++idle > 20 && !input.value) buddy.classList.add('sleepy'); }, 1000);

  window.addEventListener('pointermove', (e) => {
    wake();
    if (document.activeElement === input && input.value) return; // il lit ce que tu écris
    const r = btn.getBoundingClientRect();
    look(e.clientX - (r.left + r.width / 2), e.clientY - (r.top + r.height / 2));
  }, { passive: true });

  input.addEventListener('input', () => {
    wake();
    const r = btn.getBoundingClientRect(), ir = input.getBoundingClientRect();
    // il suit le texte qui s'allonge
    const caret = Math.min(ir.right - 10, ir.left + 14 + input.value.length * 8.5);
    look(caret - (r.left + r.width / 2), 6);
    buddy.classList.toggle('curious', !!input.value);
  });

  const happy = () => {
    if (!input.value.trim()) return;
    buddy.classList.add('happy');
    setTimeout(() => buddy.classList.remove('happy'), 1300);
  };
  btn.addEventListener('pointerdown', happy);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') happy(); });

  if (!reducedMotion) {
    (function blink() {
      buddy.classList.add('blink');
      setTimeout(() => buddy.classList.remove('blink'), 130);
      setTimeout(blink, rand(2500, 6000));
    })();
  }
})();

stage.boot();
