/* mill-fx.js — le moulin au bord de la rivière, à l'heure qu'il est chez le
   visiteur : soleil le jour, couchant, puis la lune et les fenêtres du meunier
   allumées. L'eau coule de gauche à droite et reflète vraiment ce qui la borde
   (la scène est rendue une seconde fois depuis sous la surface). Chaque
   message met à l'eau une petite barque de papier portant une bougie et donne un
   coup de roue ; tous les dix tours, la meule remplit un sac et la farine vole. */

import { THREE, rand, small, reducedMotion, glowTex, mistTex, pointFrag, makeBurst, stage, skyColor } from '/assets/fx-kit.js';

const fx = stage({ clear: 0x0d1a24, exposure: 1.05 });
const { scene, camera, renderer } = fx;
const LOOK = new THREE.Vector3(0, -1.6, -6);
const WATER = -4.5;
const HORIZON = new THREE.Color(0x2c4a62); // suit l'heure, partagé par le brouillard, le ciel et l'eau

const glow = glowTex();
const mist = mistTex();

/* ========== Le ciel : une voûte, pour que l'eau la reflète juste ========== */
const skyMat = new THREE.ShaderMaterial({
  uniforms: {
    uTop: { value: new THREE.Color() }, uMid: { value: new THREE.Color() },
    uGlow: { value: new THREE.Color() }, uHorizon: { value: HORIZON },
    uSunDir: { value: new THREE.Vector3(0, 0.2, -1) }, uSunColor: { value: new THREE.Color() }, uSun: { value: 0 },
  },
  vertexShader: `
    varying vec3 vDir;
    void main() {
      vDir = normalize(position);
      vec4 p = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
      gl_Position = p.xyww; // toujours au fond
    }`,
  fragmentShader: `
    uniform vec3 uTop; uniform vec3 uMid; uniform vec3 uGlow; uniform vec3 uHorizon;
    uniform vec3 uSunDir; uniform vec3 uSunColor; uniform float uSun;
    varying vec3 vDir;
    void main() {
      vec3 d = normalize(vDir);
      float e = d.y;
      vec3 col = mix(uHorizon, uGlow, smoothstep(-0.02, 0.06, e));
      col = mix(col, uMid, smoothstep(0.05, 0.22, e));
      col = mix(col, uTop, smoothstep(0.2, 0.7, e));
      col = mix(col, uHorizon * 0.6, smoothstep(0.0, -0.15, e));
      float s = max(dot(d, normalize(uSunDir)), 0.0);
      col += uSunColor * uSun * (pow(s, 900.0) * 6.0 + pow(s, 40.0) * 0.5 + pow(s, 6.0) * 0.18);
      gl_FragColor = vec4(col, 1.0);
      #include <tonemapping_fragment>
      #include <colorspace_fragment>
    }`,
  side: THREE.BackSide, depthWrite: false, fog: false,
});
const dome = new THREE.Mesh(new THREE.SphereGeometry(100, 32, 16), skyMat);
dome.renderOrder = -1;
scene.add(dome);
scene.fog = new THREE.Fog(HORIZON, 18, 70);

const moon = new THREE.Sprite(new THREE.SpriteMaterial({ map: glow, color: 0xdfeaff, transparent: true, opacity: 0.9, fog: false, depthWrite: false, toneMapped: false }));
moon.position.set(-14, 9, -60);
moon.scale.setScalar(9);
scene.add(moon);

const stars = new THREE.PointsMaterial({ color: 0xcfe0ff, size: 1.4, sizeAttenuation: false, transparent: true, opacity: 0.55, fog: false, depthWrite: false });
{
  const N = small ? 120 : 260;
  const pos = new Float32Array(N * 3);
  for (let i = 0; i < N; i++) pos.set([rand(-90, 90), rand(8, 45), rand(-80, -70)], i * 3);
  const geo = new THREE.BufferGeometry();
  geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  scene.add(new THREE.Points(geo, stars));
}

/* ========== Lumière ========== */
const hemi = new THREE.HemisphereLight(0x7d9cc0, 0x0d1a24, 1.4);
scene.add(hemi);
const sunLight = new THREE.DirectionalLight(0xbcd3f0, 1.2); // la lune la nuit, le soleil le jour
sunLight.position.set(-8, 10, -4);
scene.add(sunLight);
const hearth = new THREE.PointLight(0xffb560, 18, 26, 1.3);
hearth.position.set(9.5, WATER + 3.5, -13);
scene.add(hearth);

/* ========== La rivière ========== */
const LANTERNS = 6;
// le reflet : la scène vue depuis sous l'eau, en demi-résolution
const mirrorRT = new THREE.WebGLRenderTarget(1, 1, { type: THREE.HalfFloatType });
const mirrorCam = new THREE.PerspectiveCamera();
const mirrorVP = new THREE.Matrix4();
const waterClip = [new THREE.Plane(new THREE.Vector3(0, 1, 0), -WATER)];

const waterMat = new THREE.ShaderMaterial({
  uniforms: {
    uTime: { value: 0 },
    uMirror: { value: mirrorRT.texture },
    uMirrorVP: { value: mirrorVP },
    uDeep: { value: new THREE.Color(0x061219) },
    uShallow: { value: new THREE.Color(0x0e2a36) },
    uHorizon: { value: HORIZON },
    uLightPos: { value: new THREE.Vector3(-14, 9, -60) },
    uLightColor: { value: new THREE.Color(0xdfeaff) },
    uLightPower: { value: 1 },
    uMill: { value: new THREE.Vector3(9.5, WATER + 2.5, -14.5) },
    uMillPower: { value: 1 },
    uWheel: { value: new THREE.Vector3() },
    uFoamLight: { value: new THREE.Color(0x8fa6b8) },
    uBoats: { value: Array.from({ length: LANTERNS }, () => new THREE.Vector4(0, 0, 0, 0)) },
  },
  vertexShader: `
    varying vec3 vWorld;
    void main() {
      vec4 w = modelMatrix * vec4(position, 1.0);
      vWorld = w.xyz;
      gl_Position = projectionMatrix * viewMatrix * w;
    }`,
  fragmentShader: `
    uniform float uTime; uniform sampler2D uMirror; uniform mat4 uMirrorVP;
    uniform vec3 uDeep; uniform vec3 uShallow; uniform vec3 uHorizon;
    uniform vec3 uLightPos; uniform vec3 uLightColor; uniform float uLightPower;
    uniform vec3 uMill; uniform float uMillPower; uniform vec3 uWheel; uniform vec3 uFoamLight;
    uniform vec4 uBoats[${LANTERNS}];
    varying vec3 vWorld;

    // la pente de la surface : des houles qui suivent le courant, puis des rides de plus en plus fines
    vec2 g;
    void wave(vec2 p, vec2 d, float f, float a, float s) {
      d = normalize(d);
      g += d * f * a * cos(dot(d, p) * f - uTime * s);
    }
    vec3 surfaceNormal(vec2 p, float detail) {
      g = vec2(0.0);
      // on tord un peu l'espace : sans ça, la somme de vagues dessine un motif trop régulier
      p += vec2(sin(p.y * 0.37 + uTime * 0.21) * 1.3 + sin(p.y * 1.13 - uTime * 0.4) * 0.4,
                sin(p.x * 0.29 - uTime * 0.17) * 0.9 + sin(p.x * 0.91 + uTime * 0.33) * 0.35);
      wave(p, vec2(1.0, 0.25), 0.55, 0.20, 1.1);
      wave(p, vec2(1.0, -0.45), 0.9, 0.11, 1.6);
      wave(p, vec2(0.6, 1.0), 1.7, 0.05, 2.2);
      wave(p, vec2(1.0, 0.1), 2.9, 0.035 * detail, 3.1);
      wave(p, vec2(0.3, -1.0), 4.3, 0.022 * detail, 3.7);
      wave(p, vec2(1.0, 0.7), 7.1, 0.013 * detail, 5.3);
      wave(p, vec2(-0.4, 1.0), 11.3, 0.008 * detail, 6.1);
      wave(p, vec2(0.9, -0.2), 2.3, 0.04 * detail, 2.6);
      wave(p, vec2(-0.7, 0.6), 5.7, 0.016 * detail, 4.4);
      return normalize(vec3(-g.x, 1.0, -g.y));
    }

    void main() {
      vec3 toEye = cameraPosition - vWorld;
      float dist = length(toEye);
      vec3 V = toEye / dist;
      float detail = 1.0 - smoothstep(8.0, 40.0, dist);   // au loin, l'eau se calme (et ne scintille pas en moiré)
      vec3 N = normalize(mix(vec3(0.0, 1.0, 0.0), surfaceNormal(vWorld.xz, detail), 0.3 + 0.7 * detail));

      // la part du reflet selon l'angle (Fresnel, approximation de Schlick)
      float F = 0.02 + 0.98 * pow(1.0 - max(dot(N, V), 0.0), 5.0);

      // le reflet, décalé par les vagues ; plus fort en profondeur d'écran : les reflets s'étirent vers nous
      vec4 clip = uMirrorVP * vec4(vWorld, 1.0);
      vec2 uv = clip.xy / clip.w * 0.5 + 0.5;
      uv += N.xz * vec2(0.025, 0.07) * (0.35 + 0.65 * detail);
      vec3 refl = texture2D(uMirror, clamp(uv, 0.002, 0.998)).rgb;

      vec3 body = mix(uShallow, uDeep, smoothstep(2.0, 22.0, dist));
      vec3 col = mix(body, refl, clamp(F * 1.1 + 0.18, 0.0, 0.96));

      // éclats : l'astre, la fenêtre du moulin, les bougies des barques
      vec3 R = reflect(-V, N);
      float sun = max(dot(R, normalize(uLightPos - vWorld)), 0.0);
      col += uLightColor * uLightPower * (pow(sun, 420.0) * 1.8 + pow(sun, 60.0) * 0.08) * (0.3 + 0.7 * detail);
      float mill = max(dot(R, normalize(uMill - vWorld)), 0.0);
      col += vec3(1.0, 0.66, 0.3) * uMillPower * pow(mill, 140.0) * 1.6;
      for (int i = 0; i < ${LANTERNS}; i++) {
        vec4 b = uBoats[i];
        if (b.w <= 0.0) continue;
        float l = max(dot(R, normalize(b.xyz - vWorld)), 0.0);
        col += vec3(1.0, 0.7, 0.35) * pow(l, 160.0) * b.w * 1.4;
      }

      // l'écume au pied de la roue, qui file avec le courant
      vec2 w = (vWorld.xz - uWheel.xz) * vec2(0.55, 1.3);
      float near = smoothstep(2.6, 0.2, length(w - vec2(0.6, 0.0)));
      float churn = sin(vWorld.x * 6.0 - uTime * 4.0 + sin(vWorld.z * 7.0 + uTime * 2.0) * 2.0) * sin(vWorld.z * 9.0 + uTime * 3.0);
      col = mix(col, uFoamLight, near * smoothstep(0.1, 0.8, churn) * 0.55);

      col = mix(col, uHorizon, smoothstep(18.0, 70.0, dist));
      gl_FragColor = vec4(col, 1.0);
      #include <tonemapping_fragment>
      #include <colorspace_fragment>
    }`,
});
const water = new THREE.Mesh(new THREE.PlaneGeometry(160, 90, 1, 1), waterMat);
water.rotation.x = -Math.PI / 2;
water.position.set(0, WATER, -35);
scene.add(water);

// à appeler une fois la caméra placée : la même vue, symétrique par rapport à la surface
function renderMirror() {
  mirrorCam.copy(camera);
  mirrorCam.position.set(camera.position.x, 2 * WATER - camera.position.y, camera.position.z);
  mirrorCam.up.set(0, 1, 0);
  mirrorCam.lookAt(LOOK.x, 2 * WATER - LOOK.y, LOOK.z);
  mirrorCam.updateMatrixWorld();
  mirrorVP.multiplyMatrices(mirrorCam.projectionMatrix, mirrorCam.matrixWorldInverse);
  water.visible = false;
  renderer.clippingPlanes = waterClip; // rien de ce qui trempe ne se reflète
  renderer.setRenderTarget(mirrorRT);
  renderer.render(scene, mirrorCam);
  renderer.setRenderTarget(null);
  renderer.clippingPlanes = [];
  water.visible = true;
}

fx.onResize.push(() => {
  const size = renderer.getDrawingBufferSize(new THREE.Vector2());
  const k = fx.degraded ? 0.35 : 0.5;
  mirrorRT.setSize(Math.max(1, Math.round(size.x * k)), Math.max(1, Math.round(size.y * k)));
});

/* ========== L'autre rive : collines, peupliers, saule ========== */
const hillMat = new THREE.MeshStandardMaterial({ color: 0x1a2c26, roughness: 1, flatShading: true });
for (const [x, sx, sy] of [[-30, 22, 6], [-6, 18, 4], [16, 20, 5.5], [40, 24, 7]]) {
  const h = new THREE.Mesh(new THREE.SphereGeometry(1, 16, 8, 0, Math.PI * 2, 0, Math.PI / 2), hillMat);
  h.scale.set(sx, sy, 5);
  h.position.set(x, WATER - 0.2, -32 - rand(0, 6));
  scene.add(h);
}
const poplarMat = new THREE.MeshStandardMaterial({ color: 0x182a22, roughness: 1, flatShading: true });
for (let i = 0; i < (small ? 10 : 18); i++) {
  const h = rand(4, 8);
  const p = new THREE.Mesh(new THREE.ConeGeometry(rand(0.7, 1.1), h, 7), poplarMat);
  const x = rand(-36, 30);
  p.position.set(x > 3 && x < 16 ? x - 14 : x, WATER + h / 2, rand(-27, -33));
  scene.add(p);
}
// le saule près du moulin : des rideaux de branches
{
  const willow = new THREE.Group();
  const leaf = new THREE.MeshStandardMaterial({ color: 0x2b4a32, roughness: 1, flatShading: true });
  const trunk = new THREE.Mesh(new THREE.CylinderGeometry(0.25, 0.4, 4, 6), new THREE.MeshStandardMaterial({ color: 0x2a221c, roughness: 1 }));
  trunk.position.y = 2;
  willow.add(trunk);
  for (let i = 0; i < 9; i++) {
    const c = new THREE.Mesh(new THREE.ConeGeometry(rand(0.9, 1.4), rand(3.5, 4.5), 6, 1, true), leaf);
    c.rotation.x = Math.PI;
    const a = (i / 9) * Math.PI * 2;
    c.position.set(Math.cos(a) * 1.2, 3.2, Math.sin(a) * 1.2);
    willow.add(c);
  }
  willow.position.set(17, WATER, -19);
  scene.add(willow);
}

/* ========== Le moulin ========== */
const stone = new THREE.MeshStandardMaterial({ color: 0x6e6a63, roughness: 0.95, flatShading: true });
const timber = new THREE.MeshStandardMaterial({ color: 0x4a3524, roughness: 0.9, flatShading: true });
const roofMat = new THREE.MeshStandardMaterial({ color: 0x3a2f2a, roughness: 0.9, flatShading: true });
const lit = new THREE.MeshBasicMaterial({ color: 0xffc06a, toneMapped: false });

const windowHalos = [];
const mill = new THREE.Group();
mill.position.set(12.5, WATER, -16);
mill.rotation.y = -0.35;
scene.add(mill);
{
  const body = new THREE.Mesh(new THREE.BoxGeometry(6, 5, 4.5), stone);
  body.position.y = 2.5;
  mill.add(body);
  const shape = new THREE.Shape([new THREE.Vector2(-3.5, 0), new THREE.Vector2(3.5, 0), new THREE.Vector2(0, 2.8)]);
  const roof = new THREE.Mesh(new THREE.ExtrudeGeometry(shape, { depth: 5.1, bevelEnabled: false }), roofMat);
  roof.position.set(0, 5, -2.55);
  mill.add(roof);
  const chimney = new THREE.Mesh(new THREE.BoxGeometry(0.6, 1.6, 0.6), stone);
  chimney.position.set(1.6, 7, 0.8);
  mill.add(chimney);
  // fenêtres et porte allumées, côté rivière
  for (const [x, y, w, h] of [[-1.6, 3.2, 0.8, 0.9], [1.2, 3.2, 0.8, 0.9], [-0.1, 1.1, 0.9, 1.7]]) {
    const win = new THREE.Mesh(new THREE.PlaneGeometry(w, h), lit);
    win.position.set(x, y, 2.26);
    mill.add(win);
    const halo = new THREE.Sprite(new THREE.SpriteMaterial({ map: glow, color: 0xffb05a, transparent: true, opacity: 0.45, blending: THREE.AdditiveBlending, depthWrite: false, toneMapped: false }));
    halo.position.set(x, y, 2.4);
    halo.scale.setScalar(2.4);
    mill.add(halo);
    windowHalos.push(halo);
  }
}

// la roue, de face, à moitié dans l'eau
const wheel = new THREE.Group();
wheel.position.set(-5.4, 1.4, 0.6);
mill.add(wheel);
{
  const R = 2.6;
  for (const z of [-0.45, 0.45]) {
    const rim = new THREE.Mesh(new THREE.TorusGeometry(R, 0.09, 6, 40), timber);
    rim.position.z = z;
    wheel.add(rim);
    for (let i = 0; i < 8; i++) {
      const spoke = new THREE.Mesh(new THREE.BoxGeometry(0.1, R * 2, 0.1), timber);
      spoke.rotation.z = (i / 8) * Math.PI;
      spoke.position.z = z;
      wheel.add(spoke);
    }
  }
  const hub = new THREE.Mesh(new THREE.CylinderGeometry(0.35, 0.35, 1.2, 10), timber);
  hub.rotation.x = Math.PI / 2;
  wheel.add(hub);
  for (let i = 0; i < 16; i++) {
    const a = (i / 16) * Math.PI * 2;
    const paddle = new THREE.Mesh(new THREE.BoxGeometry(0.08, 0.7, 1.05), timber);
    paddle.position.set(Math.cos(a) * R, Math.sin(a) * R, 0);
    paddle.rotation.z = a;
    wheel.add(paddle);
  }
}
const wheelWorld = new THREE.Vector3();

// l'eau qui ruisselle de la roue
const SPLASH = small ? 80 : 160;
const splashGeo = new THREE.BufferGeometry();
{
  const pos = new Float32Array(SPLASH * 3), phase = new Float32Array(SPLASH), dir = new Float32Array(SPLASH * 3);
  for (let i = 0; i < SPLASH; i++) {
    phase[i] = Math.random();
    dir.set([rand(0.2, 1.4), rand(1, 2.6), rand(-0.6, 0.6)], i * 3);
  }
  splashGeo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
  splashGeo.setAttribute('aPhase', new THREE.BufferAttribute(phase, 1));
  splashGeo.setAttribute('aDir', new THREE.BufferAttribute(dir, 3));
}
const splashMat = new THREE.ShaderMaterial({
  uniforms: { uTime: { value: 0 }, uOrigin: { value: new THREE.Vector3() }, uPixel: { value: renderer.getPixelRatio() }, uPower: { value: 1 } },
  vertexShader: `
    attribute float aPhase; attribute vec3 aDir;
    uniform float uTime; uniform vec3 uOrigin; uniform float uPixel; uniform float uPower;
    varying float vLife;
    void main() {
      float t = fract(uTime * 0.9 + aPhase) * 1.1;
      vec3 p = uOrigin + aDir * t * uPower + vec3(0.0, -4.5 * t * t, 0.0);
      vec4 mv = modelViewMatrix * vec4(p, 1.0);
      gl_Position = projectionMatrix * mv;
      vLife = (1.0 - t / 1.1) * 0.8;
      gl_PointSize = uPixel * (9.0 / -mv.z) * (1.0 + aPhase);
    }`,
  fragmentShader: pointFrag('0.55, 0.75, 0.9', '0.9, 0.97, 1.0'),
  transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
});
const splash = new THREE.Points(splashGeo, splashMat);
splash.frustumCulled = false;
scene.add(splash);

/* ========== Roseaux au premier plan ========== */
const reeds = [];
{
  const mat = new THREE.MeshStandardMaterial({ color: 0x1c2c22, roughness: 1 });
  const head = new THREE.MeshStandardMaterial({ color: 0x3a2a1c, roughness: 1 });
  for (const side of [-1, 1]) {
    for (let i = 0; i < (small ? 12 : 22); i++) {
      const g = new THREE.Group();
      const h = rand(2.2, 4.2);
      const stalk = new THREE.Mesh(new THREE.CylinderGeometry(0.015, 0.04, h, 4), mat);
      stalk.position.y = h / 2;
      g.add(stalk);
      if (Math.random() < 0.45) {
        const cat = new THREE.Mesh(new THREE.CylinderGeometry(0.07, 0.07, 0.5, 6), head);
        cat.position.y = h - 0.35;
        g.add(cat);
      }
      g.position.set(side * rand(6.5, 11), WATER - 0.2, rand(0.5, 4.5));
      g.userData = { phase: rand(0, 10), lean: rand(-0.12, 0.12), baseX: g.position.x };
      scene.add(g);
      reeds.push(g);
    }
  }
}

/* ========== Brume sur l'eau ========== */
const mists = [];
for (let i = 0; i < (small ? 14 : 24); i++) {
  const s = new THREE.Sprite(new THREE.SpriteMaterial({ map: mist, transparent: true, depthWrite: false, opacity: 0, color: 0x9fb8cc, fog: false, toneMapped: false }));
  s.position.set(rand(-26, 26), WATER + rand(0.2, 1.6), rand(-28, 0));
  s.scale.set(rand(10, 18), rand(3, 5), 1);
  s.userData = { speed: rand(0.2, 0.55), op: rand(0.08, 0.18), phase: rand(0, 100) };
  scene.add(s);
  mists.push(s);
}
const mistColor = new THREE.Color();

/* ========== Les barques de papier, une bougie chacune ========== */
const paper = new THREE.MeshStandardMaterial({ color: 0xf2ead8, roughness: 0.8, side: THREE.DoubleSide });
function paperBoat() {
  const g = new THREE.Group();
  const hullShape = new THREE.Shape([
    new THREE.Vector2(-0.7, 0.25), new THREE.Vector2(0.7, 0.25), new THREE.Vector2(0.45, 0), new THREE.Vector2(-0.45, 0),
  ]);
  const hull = new THREE.Mesh(new THREE.ExtrudeGeometry(hullShape, { depth: 0.35, bevelEnabled: false }), paper);
  hull.position.z = -0.175;
  g.add(hull);
  const sail = new THREE.Mesh(new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-0.35, 0.25, 0), new THREE.Vector3(0.35, 0.25, 0), new THREE.Vector3(0, 0.75, 0),
  ]), paper);
  sail.geometry.computeVertexNormals();
  g.add(sail);
  const flame = new THREE.Sprite(new THREE.SpriteMaterial({ map: glow, color: 0xffb35a, transparent: true, opacity: 0.9, blending: THREE.AdditiveBlending, depthWrite: false, toneMapped: false }));
  flame.position.set(0.45, 0.45, 0);
  flame.scale.setScalar(0.9);
  g.add(flame);
  g.visible = false;
  g.userData = { alive: false, flame, phase: rand(0, 10), speed: 1, fade: 0 };
  scene.add(g);
  return g;
}
const boats = Array.from({ length: LANTERNS }, paperBoat);
let nextBoat = 0;

function launchBoat() {
  const b = boats[nextBoat];
  nextBoat = (nextBoat + 1) % boats.length;
  b.position.set(-12 * Math.max(fx.spread, 0.7), WATER + 0.05, rand(-7, -1.5));
  b.rotation.set(0, rand(-0.2, 0.2), 0);
  b.scale.setScalar(rand(0.85, 1.15));
  Object.assign(b.userData, { alive: true, speed: rand(1.3, 1.9), fade: 0 });
  b.visible = true;
}

/* ========== Réactions au chat ========== */
let spin = 0;       // coup de roue, retombe doucement
const flour = makeBurst(renderer, 220, pointFrag('0.85, 0.85, 0.8', '1.0, 0.98, 0.92'));
flour.material.uniforms.uFall.value = 0.3;
scene.add(flour);
const counterBurst = makeBurst(renderer, 160, pointFrag('0.5, 0.75, 0.95', '1.0, 0.85, 0.55'));
scene.add(counterBurst);
let flourStart = -99, counterStart = -99;

fx.onResize.push((px) => {
  splashMat.uniforms.uPixel.value = px;
  flour.material.uniforms.uPixel.value = counterBurst.material.uniforms.uPixel.value = px;
});

window.addEventListener('sloubi:message', (e) => {
  const m = e.detail || {};
  if (!m.live || reducedMotion) return;
  launchBoat();
  spin = Math.min(2.5, spin + 1);
});

window.addEventListener('sloubi:milestone', () => {
  if (reducedMotion) return;
  flour.material.uniforms.uOrigin.value.copy(mill.localToWorld(new THREE.Vector3(-0.1, 1.5, 2.6)));
  flourStart = fx.clock.elapsed();
  counterBurst.material.uniforms.uOrigin.value.copy(fx.screenToWorld(document.getElementById('sloubiCount')));
  counterStart = flourStart;
  fx.flareCounter();
  spin = 2.5;
});

/* ========== Boucle ========== */
let wheelAngle = 0;
const SUN = new THREE.Vector3();
const LIT_NIGHT = new THREE.Color(0xffc06a), LIT_DAY = new THREE.Color(0x4a3e34);

// le ciel, l'eau et les lumières à l'heure du visiteur
function applySky(sky) {
  // le soleil se lève à l'est (à gauche), passe au-dessus du cadre vers midi (il ne gêne pas la lecture),
  // et se couche à droite derrière les collines
  const arc = THREE.MathUtils.clamp((sky.h - 6) / 14, 0, 1);
  SUN.set(THREE.MathUtils.lerp(-40, 40, arc), -2 + 50 * Math.sin(Math.PI * arc), -60);
  const sunUp = Math.max(sky.day, sky.golden);

  skyColor(sky, '#0b1828', '#235a8a', '#24305a', skyMat.uniforms.uTop.value);
  skyColor(sky, '#22405a', '#4a80a8', '#7a5a7c', skyMat.uniforms.uMid.value);
  skyColor(sky, '#4a6a80', '#8fb4c8', '#ef9a68', skyMat.uniforms.uGlow.value);
  skyColor(sky, '#2c4a62', '#7a9fb3', '#c48a72', HORIZON);
  skyMat.uniforms.uSunDir.value.copy(SUN).normalize();
  skyColor(sky, '#000000', '#fff4dc', '#ffb070', skyMat.uniforms.uSunColor.value);
  skyMat.uniforms.uSun.value = sunUp;
  scene.fog.color.copy(HORIZON);

  moon.material.opacity = 0.9 * sky.night * (1 - sky.golden * 0.8);
  stars.opacity = 0.55 * sky.night * (1 - sky.golden * 0.9);

  skyColor(sky, '#061219', '#0f3446', '#14202c', waterMat.uniforms.uDeep.value);
  skyColor(sky, '#0e2a36', '#1f5266', '#2a2a34', waterMat.uniforms.uShallow.value);
  if (sunUp > 0.35) {
    waterMat.uniforms.uLightPos.value.copy(SUN);
    skyColor(sky, '#dfeaff', '#fff2d6', '#ffb070', waterMat.uniforms.uLightColor.value);
    waterMat.uniforms.uLightPower.value = 1 + sky.day * 1.5;
  } else {
    waterMat.uniforms.uLightPos.value.copy(moon.position);
    waterMat.uniforms.uLightColor.value.set('#dfeaff');
    waterMat.uniforms.uLightPower.value = 0.8;
  }
  waterMat.uniforms.uMillPower.value = 0.15 + 0.85 * sky.night;
  skyColor(sky, '#7d93a6', '#e6eef2', '#f0cdb4', waterMat.uniforms.uFoamLight.value);

  skyColor(sky, '#7d9cc0', '#d6e8ff', '#ffc9a0', hemi.color);
  skyColor(sky, '#0d1a24', '#3a4a3a', '#3a2a2a', hemi.groundColor);
  hemi.intensity = 1.4 + sky.day * 1.1;
  skyColor(sky, '#bcd3f0', '#fff2d8', '#ffb070', sunLight.color);
  sunLight.intensity = 1.2 + sky.day * 1.3;
  sunLight.position.copy(sunUp > 0.35 ? SUN : moon.position).normalize().multiplyScalar(20);

  lit.color.copy(LIT_NIGHT).lerp(LIT_DAY, sky.day * 0.85);
  for (const h of windowHalos) h.material.opacity = 0.45 * (0.15 + 0.85 * sky.night);
  skyColor(sky, '#9fb8cc', '#e0ecf2', '#f0c4a4', mistColor);
  renderer.toneMappingExposure = 1.05 - sky.day * 0.18;
}

fx.run((t, dt) => {
  const sky = fx.sky;
  applySky(sky);
  waterMat.uniforms.uTime.value = splashMat.uniforms.uTime.value = t;
  flour.material.uniforms.uT.value = t - flourStart;
  counterBurst.material.uniforms.uT.value = t - counterStart;

  spin = Math.max(0, spin - dt * 0.4);
  wheelAngle -= (0.35 + spin * 0.9) * dt;
  wheel.rotation.z = wheelAngle;
  splashMat.uniforms.uPower.value = 1 + spin * 0.4;

  // écran étroit : le moulin recule un peu sur la droite pour laisser le texte
  mill.position.x = 12.5 * Math.max(fx.spread, 0.75) + (1 - fx.spread) * 4;
  mill.updateMatrixWorld();
  wheel.getWorldPosition(wheelWorld);
  splashMat.uniforms.uOrigin.value.set(wheelWorld.x + 1.2, WATER + 0.3, wheelWorld.z + 0.6);
  waterMat.uniforms.uMill.value.copy(mill.localToWorld(new THREE.Vector3(-0.1, 1.6, 2.3)));

  const flick = 0.9 + 0.1 * Math.sin(t * 8.3) * Math.sin(t * 3.1);
  hearth.intensity = 18 * flick * (0.2 + 0.8 * sky.night);
  waterMat.uniforms.uWheel.value.copy(wheelWorld);

  for (let i = 0; i < boats.length; i++) {
    const b = boats[i], u = b.userData;
    const slot = waterMat.uniforms.uBoats.value[i];
    if (!u.alive) { slot.w = 0; continue; }
    b.position.x += u.speed * dt;
    b.position.y = WATER + 0.05 + Math.sin(t * 2 + u.phase) * 0.05;
    b.rotation.z = Math.sin(t * 1.6 + u.phase) * 0.06;
    b.rotation.x = Math.sin(t * 1.3 + u.phase) * 0.05;
    u.fade = Math.min(1, u.fade + dt * 0.8);
    const edge = 1 - THREE.MathUtils.smoothstep(b.position.x, 14, 20);
    u.flame.material.opacity = (0.75 + 0.25 * Math.sin(t * 11 + u.phase)) * u.fade * edge;
    slot.set(b.position.x + 0.45, WATER + 0.5, b.position.z, u.fade * edge);
    if (b.position.x > 22) { u.alive = false; b.visible = false; }
  }

  for (const r of reeds) {
    const u = r.userData;
    r.position.x = u.baseX * Math.max(fx.spread, 0.85);
    r.rotation.z = u.lean + Math.sin(t * 0.9 + u.phase) * 0.05;
  }

  for (let i = 0; i < mists.length; i++) {
    const s = mists[i], u = s.userData;
    s.visible = !(fx.degraded && i % 2);
    s.position.x += u.speed * dt;
    if (s.position.x > 30) s.position.x = -30;
    s.material.color.copy(mistColor);
    s.material.opacity = u.op * (0.7 + 0.3 * Math.sin(t * 0.25 + u.phase)) * (1 + sky.dawn * 1.4); // brume épaisse au petit matin
  }

  fx.drift(t, dt, LOOK, 1.4);
  renderMirror();
});
