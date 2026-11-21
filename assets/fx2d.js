/* fx2d.js — le socle des décors dessinés en 2D (cyberpunk, kawaii, BD) : un
   canvas à la taille de l'écran, une boucle qui s'arrête onglet caché, une
   image figée en mouvement réduit. Les décors 3D ont fx-kit.js. */

export const rand = (a, b) => a + Math.random() * (b - a);
export const pick = (a) => a[Math.floor(Math.random() * a.length)];
export const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
export const small = Math.min(window.innerWidth, window.innerHeight) < 600;

// stage2d(draw) : draw(ctx, t, dt, w, h) dessine une image, en pixels CSS
export function stage2d(draw, { fps = 60 } = {}) {
  const canvas = document.getElementById('smoke');
  const ctx = canvas.getContext('2d');
  const s = { ctx, w: 0, h: 0, dpr: 1, running: false, t0: performance.now(), last: 0, onResize: [] };
  const minFrame = 1000 / fps;

  function resize() {
    s.dpr = Math.min(window.devicePixelRatio || 1, small ? 1.5 : 2);
    s.w = window.innerWidth;
    s.h = window.innerHeight;
    canvas.width = Math.round(s.w * s.dpr);
    canvas.height = Math.round(s.h * s.dpr);
    ctx.setTransform(s.dpr, 0, 0, s.dpr, 0, 0);
    for (const f of s.onResize) f(s.w, s.h);
    if (!s.running) frame(performance.now(), true);
  }

  function frame(now, still) {
    const t = (now - s.t0) / 1000;
    const dt = still ? 0 : Math.min(0.1, (now - s.last) / 1000);
    s.last = now;
    draw(ctx, t, dt, s.w, s.h);
  }

  let raf = 0;
  function loop(now) {
    raf = requestAnimationFrame(loop);
    if (now - s.last < minFrame - 1) return; // cadence choisie : le collage bouge en « stop motion »
    frame(now);
  }

  s.start = () => {
    if (s.running || reducedMotion) return;
    s.running = true;
    s.last = performance.now();
    raf = requestAnimationFrame(loop);
  };
  s.stop = () => { s.running = false; cancelAnimationFrame(raf); };

  // un point de l'écran derrière un élément de la page
  s.centerOf = (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  };
  s.flareCounter = () => {
    const el = document.getElementById('sloubiCount');
    if (!el) return;
    el.classList.remove('flare');
    void el.offsetWidth;
    el.classList.add('flare');
  };

  window.addEventListener('resize', resize);
  document.addEventListener('visibilitychange', () => (document.hidden ? s.stop() : s.start()));
  window.addEventListener('pagehide', s.stop);
  window.addEventListener('pageshow', (e) => { if (e.persisted) s.start(); });

  s.boot = () => {
    resize();
    if (reducedMotion) frame(performance.now() + 20000, true);
    else s.start();
    requestAnimationFrame(() => document.body.classList.add('fx-ready'));
  };
  return s;
}

// une fois l'élément du message reçu, le décor peut l'habiller ; renvoie le texte du message
export function messageText(el) {
  return el?.querySelector('.message-nickname + span')?.textContent || '';
}
