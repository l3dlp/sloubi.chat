/* ring.js — la sonnerie d'appel : une petite patch de synthé modulaire tirée au
   hasard à chaque appel. Une gamme pentatonique, une voix FM (rapport et indice
   aléatoires), un filtre passe-bas balayé par un LFO, un écho. Attaques lentes,
   volume bas : ça tinte, ça ne hurle pas. Trois fois la même phrase, ~8 s.

   Le navigateur n'ouvre le son qu'après un geste : on prépare le contexte audio
   au premier clic ou à la première touche de la page. */

let ctx = null;
let current = null;

export function unlock() {
  try {
    if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)();
    if (ctx.state === 'suspended') ctx.resume().catch(() => {});
  } catch { /* pas de son, tant pis */ }
}
for (const ev of ['pointerdown', 'keydown']) window.addEventListener(ev, unlock, { passive: true });

const rnd = Math.random;
const pick = (a) => a[Math.floor(rnd() * a.length)];

const SCALES = [
  [0, 2, 4, 7, 9],    // pentatonique majeure
  [0, 3, 5, 7, 10],   // pentatonique mineure
  [0, 2, 5, 7, 9],    // suspendue
  [0, 2, 3, 7, 9],    // dorienne, sans les notes qui frottent
  [0, 4, 7, 11, 14],  // accord de septième majeure, égrené
];

// ring() : joue une sonnerie neuve. Renvoie false si le son n'est pas encore permis.
export function ring() {
  if (!ctx || ctx.state !== 'running') return false;
  stop();

  // ---- la patch du jour
  const scale = pick(SCALES);
  const root = 196 * Math.pow(2, Math.floor(rnd() * 8) / 12); // de sol 3 à ré 4
  const wave = pick(['sine', 'sine', 'triangle']);
  const ratio = pick([1, 2, 3, 3.5, 4]);       // rapport modulante / porteuse
  const index = 15 + rnd() * 110;               // profondeur de modulation, en Hz
  const step = 0.17 + rnd() * 0.12;             // pas rythmique
  const count = 4 + Math.floor(rnd() * 3);
  const phrase = [];
  let t = 0;
  for (let i = 0; i < count; i++) {
    phrase.push({ deg: pick(scale) + (rnd() < 0.3 ? 12 : 0), t, len: 0.9 + rnd() * 1.3, pan: (rnd() - 0.5) * 0.6 });
    t += step * (rnd() < 0.25 ? 1.5 : 1);
  }

  // ---- le bus : filtre balayé, écho, volume
  const master = ctx.createGain();
  const lp = ctx.createBiquadFilter();
  lp.type = 'lowpass';
  lp.frequency.value = 1500 + rnd() * 1800;
  lp.Q.value = 0.6 + rnd() * 2;
  const lfo = ctx.createOscillator();
  lfo.frequency.value = 0.15 + rnd() * 0.6;
  const lfoDepth = ctx.createGain();
  lfoDepth.gain.value = 300 + rnd() * 500;
  lfo.connect(lfoDepth).connect(lp.frequency);
  const delay = ctx.createDelay(1);
  delay.delayTime.value = Math.min(0.95, step * pick([2, 3]));
  const feedback = ctx.createGain();
  feedback.gain.value = 0.25 + rnd() * 0.2;
  const wet = ctx.createGain();
  wet.gain.value = 0.28;
  lp.connect(master);
  lp.connect(delay);
  delay.connect(feedback).connect(delay);
  delay.connect(wet).connect(master);
  master.connect(ctx.destination);

  const t0 = ctx.currentTime + 0.05;
  const gap = phrase[phrase.length - 1].t + 1.3;
  const repeats = 3;
  const end = t0 + repeats * gap + 2.5;
  master.gain.setValueAtTime(0, t0);
  master.gain.linearRampToValueAtTime(0.13, t0 + 0.4);
  master.gain.setValueAtTime(0.13, end - 1.8);
  master.gain.linearRampToValueAtTime(0, end);
  lfo.start(t0);
  lfo.stop(end + 0.1);

  for (let k = 0; k < repeats; k++) {
    // la dernière fois, la phrase monte d'une quinte : ça appelle sans insister
    const lift = k === repeats - 1 && rnd() < 0.5 ? 7 : 0;
    for (const n of phrase) voice(t0 + k * gap + n.t, root * Math.pow(2, (n.deg + lift) / 12), n.len, n.pan);
  }

  function voice(at, freq, len, panX) {
    const car = ctx.createOscillator();
    car.type = wave;
    car.frequency.value = freq;
    const mod = ctx.createOscillator();
    mod.frequency.value = freq * ratio;
    const modDepth = ctx.createGain();
    modDepth.gain.setValueAtTime(index, at);
    modDepth.gain.exponentialRampToValueAtTime(1, at + len); // le timbre s'adoucit en mourant, comme une cloche
    mod.connect(modDepth).connect(car.frequency);
    const env = ctx.createGain();
    env.gain.setValueAtTime(0.0001, at);
    env.gain.exponentialRampToValueAtTime(0.45, at + 0.04 + rnd() * 0.05);
    env.gain.exponentialRampToValueAtTime(0.0001, at + len);
    const pan = ctx.createStereoPanner ? ctx.createStereoPanner() : null;
    if (pan) {
      pan.pan.value = panX;
      car.connect(env).connect(pan).connect(lp);
    } else {
      car.connect(env).connect(lp);
    }
    car.start(at);
    mod.start(at);
    car.stop(at + len + 0.05);
    mod.stop(at + len + 0.05);
  }

  const mine = { master, lfo };
  current = mine;
  setTimeout(() => { if (current === mine) current = null; master.disconnect(); }, (end - ctx.currentTime) * 1000 + 300);
  return true;
}

// stop : on décroche (ou l'appel s'arrête) : la sonnerie s'éteint en fondu.
export function stop() {
  if (!current || !ctx) return;
  const { master, lfo } = current;
  current = null;
  master.gain.cancelScheduledValues(ctx.currentTime);
  master.gain.setTargetAtTime(0, ctx.currentTime, 0.12);
  setTimeout(() => { try { lfo.stop(); } catch {} master.disconnect(); }, 700);
}
