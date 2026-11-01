/* room-fx.js — la taverne enfumée : fumée, rais de lumière, braises, chopes et dés
   qui flottent dans le brouillard. Réagit au chat via les événements de app.js :
   un message souffle une volute, un sloubi validé fait jaillir les braises. */

import * as THREE from '/assets/vendor/three/three.module.js';

const canvas = document.getElementById('smoke');
const counterEl = document.getElementById('sloubiCount');
const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
const small = Math.min(window.innerWidth, window.innerHeight) < 600;

let renderer;
try {
  renderer = new THREE.WebGLRenderer({ canvas, antialias: false, powerPreference: 'high-performance' });
} catch {
  document.body.classList.add('no-webgl'); // le fond CSS fait office d'âtre
  throw new Error('WebGL indisponible');
}

const DPR_MAX = small ? 1.25 : 1.5;
renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, DPR_MAX));
renderer.setClearColor(0x140b07, 1);
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.1;

const scene = new THREE.Scene();
scene.fog = new THREE.FogExp2(0x1a0e08, 0.055);

const camera = new THREE.PerspectiveCamera(55, 1, 0.1, 80);
camera.position.set(0, 0, 10);
const LOOK = new THREE.Vector3(0, 0, -4);

/* ========== Textures procédurales (aucune image à charger) ========== */
function canvasTexture(size, draw, h = size) {
  const c = document.createElement('canvas');
  c.width = size;
  c.height = h;
  draw(c.getContext('2d'), size, h);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

const rand = (a, b) => a + Math.random() * (b - a);

// une volute : des dizaines de bouffées douces, masquées en disque
const smokeTex = canvasTexture(256, (g, s) => {
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

const glowTex = canvasTexture(128, (g, s) => {
  const grad = g.createRadialGradient(s / 2, s / 2, 0, s / 2, s / 2, s / 2);
  grad.addColorStop(0, 'rgba(255,255,255,1)');
  grad.addColorStop(0.25, 'rgba(255,255,255,0.45)');
  grad.addColorStop(1, 'rgba(255,255,255,0)');
  g.fillStyle = grad;
  g.fillRect(0, 0, s, s);
});

// un rai de lumière : gaussien en largeur, qui s'éteint vers le bas
const rayTex = canvasTexture(64, (g, w, h) => {
  const img = g.createImageData(w, h);
  for (let y = 0; y < h; y++) {
    const fall = Math.pow(1 - y / h, 1.4);
    for (let x = 0; x < w; x++) {
      const u = (x / (w - 1)) * 2 - 1;
      const a = Math.exp(-u * u * 4.5) * fall;
      const i = (y * w + x) * 4;
      img.data[i] = img.data[i + 1] = img.data[i + 2] = 255;
      img.data[i + 3] = a * 255;
    }
  }
  g.putImageData(img, 0, 0);
}, 512);

function dieFace(n) {
  const pips = {
    1: [[0.5, 0.5]], 2: [[0.28, 0.28], [0.72, 0.72]], 3: [[0.26, 0.26], [0.5, 0.5], [0.74, 0.74]],
    4: [[0.28, 0.28], [0.72, 0.28], [0.28, 0.72], [0.72, 0.72]],
    5: [[0.27, 0.27], [0.73, 0.27], [0.5, 0.5], [0.27, 0.73], [0.73, 0.73]],
    6: [[0.28, 0.24], [0.72, 0.24], [0.28, 0.5], [0.72, 0.5], [0.28, 0.76], [0.72, 0.76]],
  }[n];
  return canvasTexture(128, (g, s) => {
    g.fillStyle = '#e9dcc0';
    g.fillRect(0, 0, s, s);
    const vign = g.createRadialGradient(s / 2, s / 2, s * 0.3, s / 2, s / 2, s * 0.75);
    vign.addColorStop(0, 'rgba(0,0,0,0)');
    vign.addColorStop(1, 'rgba(70,40,10,0.35)');
    g.fillStyle = vign;
    g.fillRect(0, 0, s, s);
    g.fillStyle = n === 1 ? '#8a1c10' : '#2a1a10';
    for (const [x, y] of pips) {
      g.beginPath();
      g.arc(x * s, y * s, s * 0.085, 0, Math.PI * 2);
      g.fill();
    }
  });
}

/* ========== Lumière : l'âtre et les chandelles ========== */
scene.add(new THREE.AmbientLight(0x5a3418, 1.6));

const hearth = new THREE.PointLight(0xff6a24, 14, 40, 1.1);
hearth.position.set(-7, -6, 0);
scene.add(hearth);

const candle = new THREE.PointLight(0xffb35a, 3.2, 26, 1.2);
candle.position.set(6, 3, -3);
scene.add(candle);

const rim = new THREE.DirectionalLight(0xffd59a, 1.5);
rim.position.set(-4, 8, 3);
scene.add(rim);

const hearthGlow = new THREE.Sprite(new THREE.SpriteMaterial({
  map: glowTex, color: 0xff5a18, transparent: true, opacity: 0.5,
  blending: THREE.AdditiveBlending, depthWrite: false, fog: false, toneMapped: false,
}));
hearthGlow.position.set(-8, -7.5, -4);
hearthGlow.scale.setScalar(20);
scene.add(hearthGlow);

// des chandelles lointaines, floues comme un bokeh
const bokeh = [];
for (let i = 0; i < (small ? 10 : 18); i++) {
  const s = new THREE.Sprite(new THREE.SpriteMaterial({
    map: glowTex, color: new THREE.Color().setHSL(rand(0.06, 0.11), 1, rand(0.55, 0.7)),
    transparent: true, opacity: 0.5, blending: THREE.AdditiveBlending, depthWrite: false,
  }));
  s.position.set(rand(-14, 14), rand(-5, 7), rand(-22, -6));
  s.scale.setScalar(rand(0.5, 1.6));
  s.userData = { base: s.position.clone(), phase: rand(0, 100), op: rand(0.25, 0.6) };
  scene.add(s);
  bokeh.push(s);
}

/* ========== Rais de lumière tombant d'une fenêtre haute ========== */
const rays = [];
for (const [x, w, op] of [[-6, 3.2, 0.2], [-3.4, 1.6, 0.14], [-1.4, 4.2, 0.1]]) {
  const m = new THREE.Mesh(new THREE.PlaneGeometry(w, 30), new THREE.MeshBasicMaterial({
    map: rayTex, color: 0xffc77a, transparent: true, opacity: op,
    blending: THREE.AdditiveBlending, depthWrite: false, fog: false, side: THREE.DoubleSide, toneMapped: false,
  }));
  m.position.set(x, 4, -8);
  m.rotation.z = -0.42;
  m.userData = { op, phase: rand(0, 10) };
  scene.add(m);
  rays.push(m);
}

/* ========== Chopes et dés qui flottent ========== */
const wood = new THREE.MeshStandardMaterial({ color: 0x9a6a3c, roughness: 0.7, metalness: 0.05 });
const pewter = new THREE.MeshStandardMaterial({ color: 0xd8c8a8, roughness: 0.28, metalness: 0.85 });
const foam = new THREE.MeshStandardMaterial({ color: 0xfff3da, roughness: 1, emissive: 0x6a5030 });

function tankard() {
  const outer = new THREE.Group();
  const g = new THREE.Group();
  g.position.y = -0.55; // pivot au centre de la chope
  outer.add(g);
  const profile = [
    [0, 0], [0.42, 0], [0.47, 0.05], [0.45, 0.13], [0.41, 0.16], [0.42, 0.9], [0.46, 0.99],
    [0.46, 1.05], [0.4, 1.05], [0.37, 0.98], [0.36, 0.22], [0, 0.22],
  ].map(([x, y]) => new THREE.Vector2(x, y));
  g.add(new THREE.Mesh(new THREE.LatheGeometry(profile, 36), wood));
  for (const y of [0.3, 0.8]) {
    const band = new THREE.Mesh(new THREE.TorusGeometry(0.425, 0.022, 8, 40), pewter);
    band.rotation.x = Math.PI / 2;
    band.position.y = y;
    g.add(band);
  }
  const handle = new THREE.Mesh(new THREE.TorusGeometry(0.27, 0.055, 10, 24, Math.PI), pewter);
  handle.rotation.z = -Math.PI / 2;
  handle.position.set(0.44, 0.56, 0);
  g.add(handle);
  const head = new THREE.Mesh(new THREE.SphereGeometry(0.47, 24, 10, 0, Math.PI * 2, 0, Math.PI / 2), foam);
  head.scale.y = 0.6;
  head.position.y = 1.0;
  g.add(head);
  // la mousse déborde
  for (let i = 0; i < 4; i++) {
    const a = rand(0, Math.PI * 2);
    const drip = new THREE.Mesh(new THREE.SphereGeometry(rand(0.07, 0.11), 10, 8), foam);
    drip.scale.y = 2.2;
    drip.position.set(Math.cos(a) * 0.45, rand(0.82, 0.95), Math.sin(a) * 0.45);
    g.add(drip);
  }
  return outer;
}

const faces = [1, 6, 2, 5, 3, 4].map((n) => new THREE.MeshStandardMaterial({ map: dieFace(n), roughness: 0.55 }));
function die() {
  return new THREE.Mesh(new THREE.BoxGeometry(0.6, 0.6, 0.6), faces);
}

// composition : loin de la colonne de lecture, ou très loin dans la fumée
const floaters = [];
const TANKARDS = [[-8.8, 1.8, -6], [8.8, -1.2, -7], [-8.2, -3.4, -4.5], [6.4, 4, -11], [-10.5, 4.8, -14], [3.4, 6.8, -18]];
const DICE = [[7.6, -3.8, -5], [-9.6, -0.4, -9.5], [10, 2.4, -12]];
for (const [list, make, scale] of [[TANKARDS.slice(0, small ? 4 : 6), tankard, 1.25], [DICE.slice(0, small ? 2 : 3), die, 1]]) {
  for (const [x, y, z] of list) {
    const o = make();
    o.scale.setScalar(scale);
    o.rotation.set(rand(-0.5, 0.5), rand(0, Math.PI * 2), rand(-0.4, 0.4));
    o.userData = { base: new THREE.Vector3(x, y, z), phase: rand(0, 100), spin: rand(0.06, 0.18) * (Math.random() < 0.5 ? -1 : 1) };
    scene.add(o);
    floaters.push(o);
  }
}

/* ========== La fumée ========== */
const SMOKE_LOW = new THREE.Color(0xc0642c);  // éclairée par l'âtre
const SMOKE_HIGH = new THREE.Color(0x7c6a5e); // refroidie en montant
const smoke = [];
for (let i = 0; i < (small ? 34 : 60); i++) {
  const s = new THREE.Sprite(new THREE.SpriteMaterial({
    map: smokeTex, transparent: true, depthWrite: false, opacity: 0, color: 0xffffff, fog: false, toneMapped: false,
  }));
  resetSmoke(s, true);
  scene.add(s);
  smoke.push(s);
}

function resetSmoke(s, anywhere) {
  s.position.set(rand(-13, 13), anywhere ? rand(-8, 8) : rand(-9, -7), rand(-16, 3));
  s.scale.setScalar(rand(5, 11));
  s.material.rotation = rand(0, Math.PI * 2);
  s.userData = {
    speed: rand(0.18, 0.45), drift: rand(0.1, 0.35), spin: rand(-0.05, 0.05),
    op: rand(0.16, 0.34), phase: rand(0, 100), puff: 0,
  };
}

/* ========== Braises (shader) ========== */
const EMBER_FRAG = `
  varying float vLife;
  void main() {
    float d = length(gl_PointCoord - 0.5);
    float a = pow(smoothstep(0.5, 0.0, d), 1.6) * vLife;
    vec3 col = mix(vec3(1.0, 0.32, 0.06), vec3(1.0, 0.85, 0.5), vLife);
    gl_FragColor = vec4(col * a, a);
  }`;

const EMBERS = small ? 220 : 420;
const HEIGHT = 18;
const emberGeo = new THREE.BufferGeometry();
{
  const pos = new Float32Array(EMBERS * 3), phase = new Float32Array(EMBERS), speed = new Float32Array(EMBERS), size = new Float32Array(EMBERS);
  for (let i = 0; i < EMBERS; i++) {
    // plus denses près de l'âtre, en bas à gauche
    const nearHearth = Math.random() < 0.45;
    pos[i * 3] = nearHearth ? rand(-11, -2) : rand(-14, 14);
    pos[i * 3 + 1] = rand(-HEIGHT / 2, HEIGHT / 2);
    pos[i * 3 + 2] = rand(-14, 4);
    phase[i] = Math.random();
    speed[i] = rand(0.25, 0.9);
    size[i] = rand(0.9, 2.8);
  }
  emberGeo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  emberGeo.setAttribute('aPhase', new THREE.BufferAttribute(phase, 1));
  emberGeo.setAttribute('aSpeed', new THREE.BufferAttribute(speed, 1));
  emberGeo.setAttribute('aSize', new THREE.BufferAttribute(size, 1));
}
const emberMat = new THREE.ShaderMaterial({
  uniforms: { uTime: { value: 0 }, uPixel: { value: renderer.getPixelRatio() }, uHeight: { value: HEIGHT } },
  vertexShader: `
    attribute float aPhase; attribute float aSpeed; attribute float aSize;
    uniform float uTime; uniform float uPixel; uniform float uHeight;
    varying float vLife;
    void main() {
      vec3 p = position;
      p.y = mod(p.y + uTime * aSpeed + uHeight * 0.5, uHeight) - uHeight * 0.5;
      p.x += sin(uTime * 0.7 + aPhase * 6.2831) * 0.6 + sin(uTime * 1.9 + aPhase * 12.0) * 0.15;
      p.z += cos(uTime * 0.5 + aPhase * 6.2831) * 0.4;
      vec4 mv = modelViewMatrix * vec4(p, 1.0);
      gl_Position = projectionMatrix * mv;
      float flick = 0.55 + 0.45 * sin(uTime * (3.0 + aPhase * 4.0) + aPhase * 20.0);
      vLife = flick * smoothstep(uHeight * 0.5, uHeight * 0.15, p.y) * smoothstep(-uHeight * 0.5, -uHeight * 0.4, p.y);
      gl_PointSize = aSize * uPixel * (10.0 / -mv.z);
    }`,
  fragmentShader: EMBER_FRAG,
  transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
});
scene.add(new THREE.Points(emberGeo, emberMat));

// gerbe d'étincelles quand un sloubi est validé
const BURST = 200;
const burstGeo = new THREE.BufferGeometry();
{
  const pos = new Float32Array(BURST * 3), dir = new Float32Array(BURST * 3), speed = new Float32Array(BURST), size = new Float32Array(BURST);
  for (let i = 0; i < BURST; i++) {
    const a = rand(0, Math.PI * 2), up = rand(-0.2, 1);
    const v = new THREE.Vector3(Math.cos(a), up, Math.sin(a) * 0.6).normalize();
    dir.set([v.x, v.y, v.z], i * 3);
    speed[i] = rand(2.5, 7.5);
    size[i] = rand(1, 3);
  }
  burstGeo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  burstGeo.setAttribute('aDir', new THREE.BufferAttribute(dir, 3));
  burstGeo.setAttribute('aSpeed', new THREE.BufferAttribute(speed, 1));
  burstGeo.setAttribute('aSize', new THREE.BufferAttribute(size, 1));
}
const burstMat = new THREE.ShaderMaterial({
  uniforms: { uT: { value: 99 }, uOrigin: { value: new THREE.Vector3() }, uPixel: { value: renderer.getPixelRatio() } },
  vertexShader: `
    attribute vec3 aDir; attribute float aSpeed; attribute float aSize;
    uniform float uT; uniform vec3 uOrigin; uniform float uPixel;
    varying float vLife;
    void main() {
      float t = uT;
      vec3 p = uOrigin + aDir * aSpeed * (t - 0.32 * t * t) + vec3(0.0, -0.9 * t * t, 0.0);
      vec4 mv = modelViewMatrix * vec4(p, 1.0);
      gl_Position = projectionMatrix * mv;
      vLife = clamp(1.0 - t / (0.9 + aSpeed * 0.12), 0.0, 1.0);
      gl_PointSize = aSize * uPixel * (12.0 / -mv.z) * (0.6 + vLife);
    }`,
  fragmentShader: EMBER_FRAG,
  transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
});
const burst = new THREE.Points(burstGeo, burstMat);
burst.frustumCulled = false;
scene.add(burst);

/* ========== Réactions au chat ========== */
let flare = 0;
let burstStart = -99;
const clock = {
  t0: performance.now(), last: performance.now(),
  getElapsedTime() { return (performance.now() - this.t0) / 1000; },
  getDelta() { const n = performance.now(), d = (n - this.last) / 1000; this.last = n; return d; },
};

function screenToWorld(el, z = 0) {
  const r = el.getBoundingClientRect();
  const ndc = new THREE.Vector3(((r.left + r.width / 2) / window.innerWidth) * 2 - 1, -((r.top + r.height / 2) / window.innerHeight) * 2 + 1, 0.5);
  ndc.unproject(camera);
  const dir = ndc.sub(camera.position).normalize();
  return camera.position.clone().add(dir.multiplyScalar((z - camera.position.z) / dir.z));
}

function puff() {
  // trois volutes repartent du bas, plus denses
  for (let i = 0; i < 3; i++) {
    const s = smoke[Math.floor(Math.random() * smoke.length)];
    s.position.set(rand(-3, 3), rand(-6, -4), rand(-4, 2));
    s.userData.puff = 0.3;
  }
}

function sloubi() {
  burstMat.uniforms.uOrigin.value.copy(screenToWorld(counterEl));
  burstStart = clock.getElapsedTime();
  flare = 1;
  counterEl.classList.remove('flare');
  void counterEl.offsetWidth;
  counterEl.classList.add('flare');
}

window.addEventListener('sloubi:message', (e) => {
  const m = e.detail || {};
  if (!m.live || reducedMotion) return;
  puff();
  if (typeof m.sloubiNumber === 'number') sloubi();
});

/* ========== Parallaxe ========== */
const pointer = { x: 0, y: 0 };
window.addEventListener('pointermove', (e) => {
  pointer.x = (e.clientX / window.innerWidth) * 2 - 1;
  pointer.y = (e.clientY / window.innerHeight) * 2 - 1;
}, { passive: true });

/* ========== Mise à l'échelle ========== */
let spread = 1; // resserre la composition sur écran étroit
function resize() {
  const w = window.innerWidth, h = window.innerHeight;
  renderer.setSize(w, h, false);
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
  spread = THREE.MathUtils.clamp(camera.aspect / 1.6, 0.5, 1);
  emberMat.uniforms.uPixel.value = burstMat.uniforms.uPixel.value = renderer.getPixelRatio();
  if (!running) renderFrame(0);
}
window.addEventListener('resize', resize);

/* ========== Boucle ========== */
let running = false;
let frames = 0, slowFrames = 0, degraded = false;

function update(t, dt) {
  emberMat.uniforms.uTime.value = t;
  burstMat.uniforms.uT.value = t - burstStart;
  flare = Math.max(0, flare - dt * 1.4);

  const n = Math.sin(t * 7.3) * 0.5 + Math.sin(t * 13.1 + 1.7) * 0.3 + Math.sin(t * 2.1) * 0.2;
  hearth.intensity = 14 * (0.85 + 0.15 * n) + flare * 18;
  hearthGlow.material.opacity = 0.42 + 0.08 * n + flare * 0.4;
  candle.intensity = 3.2 * (0.9 + 0.1 * Math.sin(t * 9.7 + 0.4));

  for (const r of rays) r.material.opacity = r.userData.op * (0.8 + 0.2 * Math.sin(t * 0.35 + r.userData.phase)) + flare * 0.03;

  for (const b of bokeh) {
    const u = b.userData;
    b.position.set(u.base.x * spread + Math.sin(t * 0.08 + u.phase) * 0.6, u.base.y + Math.sin(t * 0.11 + u.phase) * 0.4, u.base.z);
    b.material.opacity = u.op * (0.7 + 0.3 * Math.sin(t * (1.3 + (u.phase % 1)) + u.phase));
  }

  for (const o of floaters) {
    const u = o.userData;
    // écran étroit : on ne resserre pas vers le texte, on recule dans la fumée
    o.position.set(u.base.x * Math.max(spread, 0.8), u.base.y + Math.sin(t * 0.45 + u.phase) * 0.35, u.base.z - (1 - spread) * 10);
    o.rotation.y += u.spin * dt;
    o.rotation.x = Math.sin(t * 0.3 + u.phase) * 0.25;
    o.rotation.z = Math.cos(t * 0.27 + u.phase) * 0.18;
  }

  for (let i = 0; i < smoke.length; i++) {
    const s = smoke[i], u = s.userData;
    if (degraded && i % 2) { s.visible = false; continue; }
    s.visible = true;
    s.position.y += u.speed * dt;
    s.position.x += Math.sin(t * 0.15 + u.phase) * u.drift * dt;
    s.material.rotation += u.spin * dt;
    s.scale.setScalar(s.scale.x + dt * 0.12);
    if (s.position.y > 9) resetSmoke(s, false);
    const y = s.position.y;
    const fade = THREE.MathUtils.smoothstep(y, -9, -5) * (1 - THREE.MathUtils.smoothstep(y, 4, 9));
    u.puff = Math.max(0, u.puff - dt * 0.05);
    const depth = THREE.MathUtils.clamp((s.position.z + 18) / 20, 0.35, 1); // le lointain se perd
    s.material.opacity = (u.op * fade + u.puff) * depth;
    s.material.color.copy(SMOKE_LOW).lerp(SMOKE_HIGH, THREE.MathUtils.clamp((y + 8) / 15, 0, 1));
  }

  const sway = Math.sin(t * 0.13) * 0.25;
  camera.position.x += (pointer.x * 0.9 + sway - camera.position.x) * Math.min(1, dt * 1.6);
  camera.position.y += (-pointer.y * 0.5 + Math.cos(t * 0.11) * 0.15 - camera.position.y) * Math.min(1, dt * 1.6);
  camera.lookAt(LOOK);
}

function renderFrame(t, dt = 0) {
  update(t, dt);
  renderer.render(scene, camera);
}

function loop() {
  const dt = Math.min(clock.getDelta(), 0.1);
  renderFrame(clock.getElapsedTime(), dt);

  // si la machine peine pendant les premières secondes, on allège la scène
  if (!degraded && frames < 150) {
    frames++;
    if (dt > 1 / 38) slowFrames++;
    if (frames === 150 && slowFrames > 60) {
      degraded = true;
      renderer.setPixelRatio(1);
      resize();
    }
  }
}

function start() {
  if (running || reducedMotion) return;
  running = true;
  clock.getDelta();
  renderer.setAnimationLoop(loop);
}

function stop() {
  running = false;
  renderer.setAnimationLoop(null);
}

document.addEventListener('visibilitychange', () => (document.hidden ? stop() : start()));
window.addEventListener('pagehide', stop);
window.addEventListener('pageshow', (e) => { if (e.persisted) start(); });

resize();
if (reducedMotion) {
  // la même taverne, figée : on avance la fumée jusqu'à un état posé, et une seule image
  for (let i = 0; i < 40; i++) update(20 + i * 0.25, 0.25);
  renderer.render(scene, camera);
} else {
  for (let i = 0; i < 30; i++) update(i * 0.2, 0.2); // la fumée est déjà là à l'arrivée
  start();
}
requestAnimationFrame(() => document.body.classList.add('fx-ready'));
