/* forest-fx.js — la forêt nourricière, à l'heure qu'il est chez le visiteur :
   des rais de soleil le jour, et à la tombée de la nuit des lucioles, et au sol des microbiotes qui luisent, chacun
   cerné de ses planches de culture. Un #mot fait fleurir le microbiote qui lui
   correspond ; un message fait danser les lucioles ; tous les dix messages, un
   arbre prend racine et le compteur s'illumine. */

import { THREE, rand, small, reducedMotion, glowTex, mistTex, rayTex, pointFrag, makeBurst, stage, skyColor } from '/assets/fx-kit.js';

const fx = stage({ clear: 0x08130c, exposure: 1.15 });
const { scene, camera, renderer } = fx;
scene.fog = new THREE.FogExp2(0x0b1a10, 0.045);
const LOOK = new THREE.Vector3(0, -0.6, -4);
const GROUND = -6;

const glow = glowTex();
const mist = mistTex();
const ray = rayTex();

/* ========== Lumière : un ciel vert-de-gris, des mousses qui éclairent d'en bas ========== */
const hemi = new THREE.HemisphereLight(0x5f8a6a, 0x0b1408, 1.5);
scene.add(hemi);
const moon = new THREE.DirectionalLight(0xcfe8c0, 1.3); // la lune la nuit, le soleil le jour
moon.position.set(6, 10, 4);
scene.add(moon);
const mossLight = new THREE.PointLight(0x6ee0a6, 10, 30, 1.2);
mossLight.position.set(-5, GROUND + 1.5, -3);
scene.add(mossLight);
const mossLight2 = new THREE.PointLight(0xd6e67c, 6, 26, 1.2);
mossLight2.position.set(6, GROUND + 1.5, -5);
scene.add(mossLight2);

/* ========== Le sol ========== */
const ground = new THREE.Mesh(
  new THREE.PlaneGeometry(120, 80),
  new THREE.MeshStandardMaterial({ color: 0x15281a, roughness: 1 }),
);
ground.rotation.x = -Math.PI / 2;
ground.position.set(0, GROUND, -20);
scene.add(ground);

/* ========== Les arbres ========== */
const bark = new THREE.MeshStandardMaterial({ color: 0x2c2218, roughness: 1, flatShading: true });
const leaves = [0x1f4a2a, 0x2a5a30, 0x173d24, 0x355f2c].map((c) =>
  new THREE.MeshStandardMaterial({ color: c, roughness: 0.95, flatShading: true }));
const fruit = new THREE.MeshStandardMaterial({ color: 0xffb35a, emissive: 0xc0501a, emissiveIntensity: 0.6, roughness: 0.5 });

function tree(h, r, withFruit) {
  const g = new THREE.Group();
  const trunk = new THREE.Mesh(new THREE.CylinderGeometry(r * 0.55, r, h, 7, 3), bark);
  trunk.position.y = h / 2;
  g.add(trunk);
  const crowns = 3 + Math.floor(Math.random() * 3);
  for (let i = 0; i < crowns; i++) {
    const s = rand(1.2, 2.2) * (h / 9);
    const c = new THREE.Mesh(new THREE.IcosahedronGeometry(s, 1), leaves[Math.floor(Math.random() * leaves.length)]);
    c.position.set(rand(-1.2, 1.2) * (h / 9), h * rand(0.82, 1.05), rand(-1, 1) * (h / 9));
    c.scale.y = rand(0.7, 0.95);
    g.add(c);
    // la forêt nourricière : quelques fruits accrochés sous les couronnes
    if (withFruit) {
      for (let k = 0; k < 4; k++) {
        const f = new THREE.Mesh(new THREE.SphereGeometry(rand(0.09, 0.14), 8, 6), fruit);
        const a = rand(0, Math.PI * 2);
        f.position.set(c.position.x + Math.cos(a) * s * 0.8, c.position.y - s * rand(0.3, 0.6), c.position.z + Math.sin(a) * s * 0.8);
        g.add(f);
      }
    }
  }
  return g;
}

// premier plan sur les côtés, puis des rangs qui se perdent dans la brume
const trees = [];
const NEAR = [[-11.5, -4], [12, -6], [-14, -11], [15, -13], [-8.5, -16], [9, -19]];
for (const [x, z] of NEAR) {
  const t = tree(rand(11, 15), rand(0.45, 0.7), Math.random() < 0.6);
  t.position.set(x, GROUND, z);
  t.rotation.y = rand(0, Math.PI * 2);
  t.userData = { base: x };
  scene.add(t);
  trees.push(t);
}
for (let i = 0; i < (small ? 14 : 26); i++) {
  const t = tree(rand(9, 16), rand(0.35, 0.6), false);
  const x = rand(-30, 30);
  t.position.set(Math.abs(x) < 5 ? x * 3 : x, GROUND, rand(-24, -48));
  t.rotation.y = rand(0, Math.PI * 2);
  t.userData = { base: t.position.x };
  scene.add(t);
  trees.push(t);
}

// le dernier arbre planté (tous les dix messages) pousse en premier plan
const sapling = tree(9, 0.4, true);
sapling.position.set(-6.8, GROUND, -9);
sapling.scale.setScalar(0.001);
sapling.userData = { grow: 0, target: 0.001 };
scene.add(sapling);

/* ========== Rais de lumière à travers la canopée ========== */
const rays = [];
for (const [x, w, op] of [[4, 3, 0.14], [7, 1.8, 0.11], [1.5, 4.5, 0.07], [-3, 2, 0.06]]) {
  const m = new THREE.Mesh(new THREE.PlaneGeometry(w, 34), new THREE.MeshBasicMaterial({
    map: ray, color: 0xe6f2a8, transparent: true, opacity: op,
    blending: THREE.AdditiveBlending, depthWrite: false, fog: false, side: THREE.DoubleSide, toneMapped: false,
  }));
  m.position.set(x, 5, -10);
  m.rotation.z = 0.32;
  m.userData = { op, phase: rand(0, 10) };
  scene.add(m);
  rays.push(m);
}

/* ========== Les microbiotes : mousses, champignons, et leurs planches en cercle ========== */
const BIOME_COLORS = [0x6ee0a6, 0xd6e67c, 0x9fb7ff, 0xffb35a, 0xe39cff, 0x7fe0e0];
const BIOMES = [[-6, -3], [5.5, -4.5], [-2.5, -9], [9, -11], [-10, -12], [2.5, -15]];
const biomes = [];
const stemMat = new THREE.MeshStandardMaterial({ color: 0xd8d0b8, roughness: 0.8, emissive: 0x2a2a20 });

for (let i = 0; i < BIOMES.length; i++) {
  const [x, z] = BIOMES[i];
  const color = new THREE.Color(BIOME_COLORS[i % BIOME_COLORS.length]);
  const g = new THREE.Group();
  g.position.set(x, GROUND, z);
  const cap = new THREE.MeshStandardMaterial({ color, emissive: color, emissiveIntensity: 0.9, roughness: 0.4 });
  for (let k = 0; k < (small ? 5 : 8); k++) {
    const m = new THREE.Group();
    const h = rand(0.25, 0.7);
    const stem = new THREE.Mesh(new THREE.CylinderGeometry(0.04, 0.06, h, 6), stemMat);
    stem.position.y = h / 2;
    const top = new THREE.Mesh(new THREE.SphereGeometry(rand(0.12, 0.26), 12, 6, 0, Math.PI * 2, 0, Math.PI / 2), cap);
    top.position.y = h;
    m.add(stem, top);
    const a = rand(0, Math.PI * 2), d = rand(0, 1.1);
    m.position.set(Math.cos(a) * d, 0, Math.sin(a) * d);
    m.rotation.z = rand(-0.2, 0.2);
    g.add(m);
  }
  // les planches en cercle, comme un jardin en trou de serrure
  const ringMat = new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.22, blending: THREE.AdditiveBlending, depthWrite: false });
  for (const r of [1.8, 2.6]) {
    const ring = new THREE.Mesh(new THREE.RingGeometry(r, r + 0.05, 64, 1, 0.35, Math.PI * 2 - 0.7), ringMat);
    ring.rotation.x = -Math.PI / 2;
    ring.rotation.z = rand(0, Math.PI * 2);
    ring.position.y = 0.02;
    g.add(ring);
  }
  const halo = new THREE.Sprite(new THREE.SpriteMaterial({
    map: glow, color, transparent: true, opacity: 0.35, blending: THREE.AdditiveBlending, depthWrite: false, toneMapped: false,
  }));
  halo.position.y = 0.6;
  halo.scale.setScalar(4.5);
  g.add(halo);
  g.userData = { halo, ringMat, cap, bloom: 0, phase: rand(0, 10) };
  scene.add(g);
  biomes.push(g);
}

// une spirale d'aromatiques au premier plan, tracée en lumière
{
  const pts = [];
  for (let a = 0; a < Math.PI * 6; a += 0.08) {
    const r = 0.15 + a * 0.12;
    pts.push(new THREE.Vector3(Math.cos(a) * r, 0.03, Math.sin(a) * r));
  }
  const spiral = new THREE.Line(new THREE.BufferGeometry().setFromPoints(pts),
    new THREE.LineBasicMaterial({ color: 0xd6e67c, transparent: true, opacity: 0.4, blending: THREE.AdditiveBlending }));
  spiral.position.set(3.2, GROUND, -1.5);
  scene.add(spiral);
}

/* ========== Brume au ras du sol ========== */
const MIST_LOW = new THREE.Color(0x6fa884);
const mists = [];
for (let i = 0; i < (small ? 16 : 28); i++) {
  const s = new THREE.Sprite(new THREE.SpriteMaterial({ map: mist, transparent: true, depthWrite: false, opacity: 0, color: MIST_LOW, fog: false, toneMapped: false }));
  s.position.set(rand(-20, 20), GROUND + rand(0.3, 2.5), rand(-30, 2));
  s.scale.setScalar(rand(6, 12));
  s.material.rotation = rand(0, Math.PI * 2);
  s.userData = { speed: rand(0.1, 0.35) * (Math.random() < 0.5 ? -1 : 1), op: rand(0.1, 0.22), phase: rand(0, 100) };
  scene.add(s);
  mists.push(s);
}

/* ========== Lucioles ========== */
const FIREFLIES = small ? 90 : 180;
const flyGeo = new THREE.BufferGeometry();
{
  const pos = new Float32Array(FIREFLIES * 3), phase = new Float32Array(FIREFLIES), size = new Float32Array(FIREFLIES);
  for (let i = 0; i < FIREFLIES; i++) {
    pos.set([rand(-16, 16), rand(GROUND + 0.5, 5), rand(-22, 4)], i * 3);
    phase[i] = Math.random();
    size[i] = rand(1, 2.6);
  }
  flyGeo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  flyGeo.setAttribute('aPhase', new THREE.BufferAttribute(phase, 1));
  flyGeo.setAttribute('aSize', new THREE.BufferAttribute(size, 1));
}
const flyMat = new THREE.ShaderMaterial({
  uniforms: { uTime: { value: 0 }, uPixel: { value: renderer.getPixelRatio() }, uGlow: { value: 0 }, uShow: { value: 1 } },
  vertexShader: `
    attribute float aPhase; attribute float aSize;
    uniform float uTime; uniform float uPixel; uniform float uGlow; uniform float uShow;
    varying float vLife;
    void main() {
      vec3 p = position;
      float k = aPhase * 6.2831;
      p.x += sin(uTime * 0.31 + k) * 1.4 + sin(uTime * 0.9 + k * 3.0) * 0.3;
      p.y += sin(uTime * 0.43 + k * 2.0) * 0.8;
      p.z += cos(uTime * 0.27 + k) * 1.2;
      vec4 mv = modelViewMatrix * vec4(p, 1.0);
      gl_Position = projectionMatrix * mv;
      float blink = pow(max(0.0, sin(uTime * (0.7 + aPhase) + k * 5.0)), 3.0);
      vLife = clamp(0.15 + blink * 0.85 + uGlow, 0.0, 1.0) * uShow; // les lucioles sortent le soir
      gl_PointSize = aSize * uPixel * (10.0 / -mv.z) * (1.0 + uGlow * 0.6);
    }`,
  fragmentShader: pointFrag('0.45, 0.9, 0.55', '0.95, 1.0, 0.55'),
  transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
});
scene.add(new THREE.Points(flyGeo, flyMat));

/* ========== Spores qui tombent doucement ========== */
const SPORES = small ? 140 : 260;
const HEIGHT = 16;
const sporeGeo = new THREE.BufferGeometry();
{
  const pos = new Float32Array(SPORES * 3), phase = new Float32Array(SPORES), speed = new Float32Array(SPORES), size = new Float32Array(SPORES);
  for (let i = 0; i < SPORES; i++) {
    pos.set([rand(-16, 16), rand(-HEIGHT / 2, HEIGHT / 2), rand(-18, 4)], i * 3);
    phase[i] = Math.random();
    speed[i] = rand(0.08, 0.3);
    size[i] = rand(0.5, 1.2);
  }
  sporeGeo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  sporeGeo.setAttribute('aPhase', new THREE.BufferAttribute(phase, 1));
  sporeGeo.setAttribute('aSpeed', new THREE.BufferAttribute(speed, 1));
  sporeGeo.setAttribute('aSize', new THREE.BufferAttribute(size, 1));
}
const sporeMat = new THREE.ShaderMaterial({
  uniforms: { uTime: { value: 0 }, uPixel: { value: renderer.getPixelRatio() }, uHeight: { value: HEIGHT }, uShow: { value: 1 } },
  vertexShader: `
    attribute float aPhase; attribute float aSpeed; attribute float aSize;
    uniform float uTime; uniform float uPixel; uniform float uHeight; uniform float uShow;
    varying float vLife;
    void main() {
      vec3 p = position;
      p.y = uHeight * 0.5 - mod(uHeight * 0.5 - p.y + uTime * aSpeed, uHeight);
      p.x += sin(uTime * 0.4 + aPhase * 6.2831) * 0.8;
      vec4 mv = modelViewMatrix * vec4(p, 1.0);
      gl_Position = projectionMatrix * mv;
      vLife = 0.35 * uShow * smoothstep(uHeight * 0.5, uHeight * 0.3, p.y) * smoothstep(-uHeight * 0.5, -uHeight * 0.35, p.y);
      gl_PointSize = aSize * uPixel * (10.0 / -mv.z);
    }`,
  fragmentShader: pointFrag('0.8, 0.85, 0.6', '1.0, 0.95, 0.8'),
  transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
});
scene.add(new THREE.Points(sporeGeo, sporeMat));

// gerbes : au compteur (un arbre a pris racine), et au-dessus d'un microbiote qui fleurit
const counterBurst = makeBurst(renderer, 200, pointFrag('0.4, 0.9, 0.6', '0.95, 1.0, 0.6'));
const biomeBurst = makeBurst(renderer, 120, pointFrag('0.5, 0.95, 0.7', '1.0, 1.0, 0.8'));
biomeBurst.material.uniforms.uFall.value = -0.4; // les spores montent
scene.add(counterBurst, biomeBurst);
let counterStart = -99, biomeStart = -99;

fx.onResize.push((px) => {
  flyMat.uniforms.uPixel.value = sporeMat.uniforms.uPixel.value = px;
  counterBurst.material.uniforms.uPixel.value = biomeBurst.material.uniforms.uPixel.value = px;
});

/* ========== Réactions au chat ========== */
let glowBoost = 0;

function hashTag(tag) {
  let h = 0;
  for (const ch of tag) h = (h * 31 + ch.codePointAt(0)) >>> 0;
  return h % biomes.length;
}

window.addEventListener('sloubi:message', (e) => {
  const m = e.detail || {};
  if (!m.live || reducedMotion) return;
  glowBoost = 1;
  for (const tag of m.tags || []) {
    const b = biomes[hashTag(tag)];
    b.userData.bloom = 1;
    biomeBurst.material.uniforms.uOrigin.value.copy(b.position).add(new THREE.Vector3(0, 0.6, 0));
    biomeStart = fx.clock.elapsed();
  }
});

// l'arbre du premier plan grandit avec la conversation : un quart de plus tous les dix messages
window.addEventListener('sloubi:count', (e) => {
  const trees = Math.floor((e.detail?.count || 0) / 10);
  sapling.userData.target = Math.max(0.001, Math.min(1, trees / 4));
  if (!e.detail?.live || reducedMotion) sapling.userData.grow = sapling.userData.target;
});

window.addEventListener('sloubi:milestone', () => {
  if (reducedMotion) return;
  counterBurst.material.uniforms.uOrigin.value.copy(fx.screenToWorld(document.getElementById('sloubiCount')));
  counterStart = fx.clock.elapsed();
  fx.flareCounter();
  for (const b of biomes) b.userData.bloom = Math.max(b.userData.bloom, 0.6);
});

/* ========== L'heure du visiteur ========== */
const fogColor = new THREE.Color(), mistColor = new THREE.Color();
let glowK = 1; // les microbiotes luisent surtout la nuit

function applySky(sky) {
  skyColor(sky, '#0b1a10', '#3e5c3a', '#5a4a30', fogColor);
  scene.fog.color.copy(fogColor);
  renderer.setClearColor(fogColor);
  skyColor(sky, '#5f8a6a', '#dcefc4', '#ffd09a', hemi.color);
  skyColor(sky, '#0b1408', '#2a3a1c', '#2a2010', hemi.groundColor);
  hemi.intensity = 1.5 + sky.day * 1.6;
  skyColor(sky, '#cfe8c0', '#fff1cc', '#ffb070', moon.color);
  moon.intensity = 1.3 + sky.day * 1.7;
  // le soleil passe d'est en ouest ; au couchant, il rase le sous-bois
  const arc = THREE.MathUtils.clamp((sky.h - 6) / 14, 0, 1);
  moon.position.set(sky.day > 0.3 ? THREE.MathUtils.lerp(-12, 12, arc) : 6, 4 + 8 * Math.sin(Math.PI * arc), 4);
  for (const r of rays) skyColor(sky, '#e6f2a8', '#fff6c8', '#ffc070', r.material.color);
  glowK = 0.2 + 0.8 * Math.min(1, sky.night + sky.golden * 0.5);
  flyMat.uniforms.uShow.value = Math.max(0.04, Math.min(1, sky.night + sky.golden * 0.35));
  sporeMat.uniforms.uShow.value = 0.6 + sky.day * 1.2; // le pollen se voit dans le soleil
  skyColor(sky, '#6fa884', '#c8dcc0', '#e0c090', mistColor);
  renderer.toneMappingExposure = 1.15 - sky.day * 0.12;
}

/* ========== Boucle ========== */
fx.run((t, dt) => {
  const sky = fx.sky;
  applySky(sky);
  flyMat.uniforms.uTime.value = sporeMat.uniforms.uTime.value = t;
  glowBoost = Math.max(0, glowBoost - dt * 0.8);
  flyMat.uniforms.uGlow.value = glowBoost * 0.6;
  counterBurst.material.uniforms.uT.value = t - counterStart;
  biomeBurst.material.uniforms.uT.value = t - biomeStart;

  const breathe = 0.5 + 0.5 * Math.sin(t * 0.6);
  mossLight.intensity = (8 + 3 * breathe) * glowK + glowBoost * 6;
  mossLight2.intensity = (5 + 2 * Math.sin(t * 0.5 + 1.3)) * glowK;

  const rayK = 0.6 + sky.day * 1.5 + sky.golden * 1.6;
  for (const r of rays) r.material.opacity = r.userData.op * rayK * (0.75 + 0.25 * Math.sin(t * 0.3 + r.userData.phase));

  for (const b of biomes) {
    const u = b.userData;
    u.bloom = Math.max(0, u.bloom - dt * 0.35);
    const pulse = 0.5 + 0.5 * Math.sin(t * 0.8 + u.phase);
    u.halo.material.opacity = (0.22 + 0.12 * pulse) * glowK + u.bloom * 0.6;
    u.halo.scale.setScalar(4.5 + u.bloom * 4);
    u.ringMat.opacity = (0.16 + 0.08 * pulse) * glowK + u.bloom * 0.5;
    u.cap.emissiveIntensity = (0.7 + 0.4 * pulse) * glowK + u.bloom * 2;
    b.scale.setScalar(1 + u.bloom * 0.35);
  }

  const sp = sapling.userData;
  sp.grow += (sp.target - sp.grow) * Math.min(1, dt * 0.6);
  sapling.scale.setScalar(Math.max(0.001, sp.grow));

  // écran étroit : les arbres du premier plan s'écartent du texte
  for (const tr of trees) tr.position.x = tr.userData.base * (tr.position.z > -20 ? Math.max(fx.spread, 0.8) : 1);

  for (let i = 0; i < mists.length; i++) {
    const s = mists[i], u = s.userData;
    s.visible = !(fx.degraded && i % 2);
    s.position.x += u.speed * dt;
    if (s.position.x > 22) s.position.x = -22;
    if (s.position.x < -22) s.position.x = 22;
    s.material.rotation += u.speed * 0.02 * dt;
    s.material.color.copy(mistColor);
    s.material.opacity = u.op * (0.7 + 0.3 * Math.sin(t * 0.2 + u.phase)) * (1 + sky.dawn * 1.2); // brume du petit matin
  }

  fx.drift(t, dt, LOOK);
});
