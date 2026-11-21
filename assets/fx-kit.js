/* fx-kit.js — le socle commun des décors forêt et moulin : rendu, textures
   procédurales, parallaxe, boucle qui s'allège si la machine peine, image figée
   en mouvement réduit. room-fx.js (la taverne) reste autonome. */

import * as THREE from '/assets/vendor/three/three.module.js';

export { THREE };

export const rand = (a, b) => a + Math.random() * (b - a);
export const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
export const small = Math.min(window.innerWidth, window.innerHeight) < 600;

export function canvasTexture(size, draw, h = size) {
  const c = document.createElement('canvas');
  c.width = size;
  c.height = h;
  draw(c.getContext('2d'), size, h);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

export const glowTex = () => canvasTexture(128, (g, s) => {
  const grad = g.createRadialGradient(s / 2, s / 2, 0, s / 2, s / 2, s / 2);
  grad.addColorStop(0, 'rgba(255,255,255,1)');
  grad.addColorStop(0.25, 'rgba(255,255,255,0.45)');
  grad.addColorStop(1, 'rgba(255,255,255,0)');
  g.fillStyle = grad;
  g.fillRect(0, 0, s, s);
});

// une nappe de brume : des bouffées douces masquées en disque
export const mistTex = () => canvasTexture(256, (g, s) => {
  for (let i = 0; i < 46; i++) {
    const r = rand(s * 0.08, s * 0.24);
    const a = rand(0, Math.PI * 2), d = rand(0, s * 0.24);
    const x = s / 2 + Math.cos(a) * d, y = s / 2 + Math.sin(a) * d;
    const grad = g.createRadialGradient(x, y, 0, x, y, r);
    grad.addColorStop(0, `rgba(255,255,255,${rand(0.05, 0.14)})`);
    grad.addColorStop(1, 'rgba(255,255,255,0)');
    g.fillStyle = grad;
    g.fillRect(0, 0, s, s);
  }
  g.globalCompositeOperation = 'destination-in';
  const mask = g.createRadialGradient(s / 2, s / 2, s * 0.1, s / 2, s / 2, s / 2);
  mask.addColorStop(0, 'rgba(0,0,0,1)');
  mask.addColorStop(1, 'rgba(0,0,0,0)');
  g.fillStyle = mask;
  g.fillRect(0, 0, s, s);
});

// un rai de lumière : gaussien en largeur, qui s'éteint vers le bas
export const rayTex = () => canvasTexture(64, (g, w, h) => {
  const img = g.createImageData(w, h);
  for (let y = 0; y < h; y++) {
    const fall = Math.pow(1 - y / h, 1.4);
    for (let x = 0; x < w; x++) {
      const u = (x / (w - 1)) * 2 - 1;
      const i = (y * w + x) * 4;
      img.data[i] = img.data[i + 1] = img.data[i + 2] = 255;
      img.data[i + 3] = Math.exp(-u * u * 4.5) * fall * 255;
    }
  }
  g.putImageData(img, 0, 0);
}, 512);

// le fragment des particules lumineuses : un point doux, teinté entre deux couleurs
export const pointFrag = (a, b) => `
  varying float vLife;
  void main() {
    float d = length(gl_PointCoord - 0.5);
    float al = pow(smoothstep(0.5, 0.0, d), 1.6) * vLife;
    vec3 col = mix(vec3(${a}), vec3(${b}), vLife);
    gl_FragColor = vec4(col * al, al);
  }`;

/* ========== L'heure qu'il est chez le visiteur ========== */
// ?heure=14 force l'heure (pour essayer les ambiances sans attendre le soir)
const forcedHour = (() => {
  const q = new URLSearchParams(location.search).get('heure');
  if (q === null || q === '' || isNaN(+q)) return null;
  return ((+q % 24) + 24) % 24;
})();
const smooth = (a, b, x) => { const t = Math.min(1, Math.max(0, (x - a) / (b - a))); return t * t * (3 - 2 * t); };
const bump = (x, c, w) => Math.exp(-(((x - c) / w) ** 2));

// day : 0 la nuit, 1 en plein jour ; golden : l'aube et le couchant, autour de 7 h et 19 h
export function daylight() {
  const d = new Date();
  const h = forcedHour ?? d.getHours() + d.getMinutes() / 60 + d.getSeconds() / 3600;
  const day = smooth(5.5, 8.5, h) * (1 - smooth(17.5, 20.5, h));
  const dawn = bump(h, 7, 1.2), dusk = bump(h, 19, 1.3);
  const golden = Math.max(dawn, dusk);
  const name = day > 0.6 ? 'jour' : golden > 0.4 ? (h < 12 ? 'aube' : 'soir') : 'nuit';
  return { h, day, night: 1 - day, golden, dawn, dusk, name };
}

// une couleur selon l'heure : celle de la nuit, du jour, et de l'aube ou du couchant
export function skyColor(sky, night, day, golden, out = new THREE.Color()) {
  return out.set(night).lerp(_c.set(day), sky.day).lerp(_c.set(golden), sky.golden * 0.85);
}
const _c = new THREE.Color();

/* Une gerbe de particules qui part d'un point (le compteur, une roue…) */
export function makeBurst(renderer, count, frag) {
  const geo = new THREE.BufferGeometry();
  const pos = new Float32Array(count * 3), dir = new Float32Array(count * 3), speed = new Float32Array(count), size = new Float32Array(count);
  for (let i = 0; i < count; i++) {
    const a = rand(0, Math.PI * 2), up = rand(-0.2, 1);
    const v = new THREE.Vector3(Math.cos(a), up, Math.sin(a) * 0.6).normalize();
    dir.set([v.x, v.y, v.z], i * 3);
    speed[i] = rand(2.5, 7.5);
    size[i] = rand(1, 3);
  }
  geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  geo.setAttribute('aDir', new THREE.BufferAttribute(dir, 3));
  geo.setAttribute('aSpeed', new THREE.BufferAttribute(speed, 1));
  geo.setAttribute('aSize', new THREE.BufferAttribute(size, 1));
  const mat = new THREE.ShaderMaterial({
    uniforms: { uT: { value: 99 }, uOrigin: { value: new THREE.Vector3() }, uPixel: { value: renderer.getPixelRatio() }, uFall: { value: 0.9 } },
    vertexShader: `
      attribute vec3 aDir; attribute float aSpeed; attribute float aSize;
      uniform float uT; uniform vec3 uOrigin; uniform float uPixel; uniform float uFall;
      varying float vLife;
      void main() {
        float t = uT;
        vec3 p = uOrigin + aDir * aSpeed * (t - 0.32 * t * t) + vec3(0.0, -uFall * t * t, 0.0);
        vec4 mv = modelViewMatrix * vec4(p, 1.0);
        gl_Position = projectionMatrix * mv;
        vLife = clamp(1.0 - t / (0.9 + aSpeed * 0.12), 0.0, 1.0);
        gl_PointSize = aSize * uPixel * (12.0 / -mv.z) * (0.6 + vLife);
      }`,
    fragmentShader: frag,
    transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
  });
  const points = new THREE.Points(geo, mat);
  points.frustumCulled = false;
  return points;
}

/* La scène : rendu, caméra, parallaxe, boucle. update(t, dt, ctx) dessine une image. */
export function stage({ clear, exposure = 1.1 }) {
  const canvas = document.getElementById('smoke');
  let renderer;
  try {
    renderer = new THREE.WebGLRenderer({ canvas, antialias: false, powerPreference: 'high-performance' });
  } catch {
    document.body.classList.add('no-webgl'); // le fond CSS prend le relais
    throw new Error('WebGL indisponible');
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, small ? 1.25 : 1.5));
  renderer.setClearColor(clear, 1);
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = exposure;

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(55, 1, 0.1, 120);
  camera.position.set(0, 0, 10);

  const pointer = { x: 0, y: 0 };
  window.addEventListener('pointermove', (e) => {
    pointer.x = (e.clientX / window.innerWidth) * 2 - 1;
    pointer.y = (e.clientY / window.innerHeight) * 2 - 1;
  }, { passive: true });

  const clock = {
    t0: performance.now(), last: performance.now(),
    elapsed() { return (performance.now() - this.t0) / 1000; },
    delta() { const n = performance.now(), d = (n - this.last) / 1000; this.last = n; return d; },
  };

  const ctx = {
    THREE, renderer, scene, camera, pointer, clock,
    sky: daylight(),
    spread: 1, degraded: false, running: false,
    onResize: [],
    // un point du monde derrière un élément de la page (pour faire partir une gerbe du compteur)
    screenToWorld(el, z = 0) {
      const r = el.getBoundingClientRect();
      const ndc = new THREE.Vector3(((r.left + r.width / 2) / window.innerWidth) * 2 - 1, -((r.top + r.height / 2) / window.innerHeight) * 2 + 1, 0.5);
      ndc.unproject(camera);
      const dir = ndc.sub(camera.position).normalize();
      return camera.position.clone().add(dir.multiplyScalar((z - camera.position.z) / dir.z));
    },
    flareCounter() {
      const el = document.getElementById('sloubiCount');
      if (!el) return;
      el.classList.remove('flare');
      void el.offsetWidth;
      el.classList.add('flare');
    },
  };

  ctx.run = (update) => {
    let frames = 0, slow = 0, skyAt = 0;
    // l'heure ne bouge pas vite : on la relit toutes les 5 s
    const tick = () => {
      if (performance.now() - skyAt < 5000) return;
      skyAt = performance.now();
      ctx.sky = daylight();
      document.body.dataset.daylight = ctx.sky.name; // la CSS garde le texte lisible le jour
    };
    const draw = (t, dt) => { tick(); update(t, dt); renderer.render(scene, camera); };

    function resize() {
      const w = window.innerWidth, h = window.innerHeight;
      renderer.setSize(w, h, false);
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
      ctx.spread = THREE.MathUtils.clamp(camera.aspect / 1.6, 0.5, 1);
      for (const f of ctx.onResize) f(renderer.getPixelRatio());
      if (!ctx.running) draw(reducedMotion ? 30 : 0, 0);
    }

    function loop() {
      const dt = Math.min(clock.delta(), 0.1);
      draw(clock.elapsed(), dt);
      // si la machine peine pendant les premières secondes, on allège
      if (!ctx.degraded && frames < 150) {
        frames++;
        if (dt > 1 / 38) slow++;
        if (frames === 150 && slow > 60) {
          ctx.degraded = true;
          renderer.setPixelRatio(1);
          resize();
        }
      }
    }

    const start = () => {
      if (ctx.running || reducedMotion) return;
      ctx.running = true;
      clock.delta();
      renderer.setAnimationLoop(loop);
    };
    const stop = () => { ctx.running = false; renderer.setAnimationLoop(null); };

    window.addEventListener('resize', resize);
    document.addEventListener('visibilitychange', () => (document.hidden ? stop() : start()));
    window.addEventListener('pagehide', stop);
    window.addEventListener('pageshow', (e) => { if (e.persisted) start(); });

    resize();
    if (reducedMotion) {
      // le même décor, figé dans un état posé
      tick();
      for (let i = 0; i < 40; i++) update(20 + i * 0.25, 0.25);
      renderer.render(scene, camera);
    } else {
      tick();
      for (let i = 0; i < 30; i++) update(i * 0.2, 0.2); // la brume est déjà là à l'arrivée
      start();
    }
    requestAnimationFrame(() => document.body.classList.add('fx-ready'));
  };

  // la caméra flotte doucement et suit un peu le pointeur
  ctx.drift = (t, dt, look, baseY = 0) => {
    const sway = Math.sin(t * 0.13) * 0.25;
    camera.position.x += (pointer.x * 0.9 + sway - camera.position.x) * Math.min(1, dt * 1.6);
    camera.position.y += (-pointer.y * 0.5 + Math.cos(t * 0.11) * 0.15 + baseY - camera.position.y) * Math.min(1, dt * 1.6);
    camera.lookAt(look);
  };

  return ctx;
}
