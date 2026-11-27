// camfx.js — sa caméra, pour le live (call.js) et l'enregistreur (recorder.js) :
// passer d'une caméra à l'autre quand il y en a plusieurs, et régler son image (clarté,
// luminosité, noir et blanc, teinte). Les réglages s'appliquent au flux envoyé : les autres
// voient la même image que soi. Ils sont gardés dans ce navigateur (sloubi_camfx).
//
// Sans réglage, on envoie la piste de la caméra telle quelle (rien à calculer). Avec un
// réglage, l'image passe par un canvas (ctx.filter) dont on envoie captureStream().
// « stable » (l'enregistreur) : toujours le canvas, pour que la piste ne change jamais
// pendant un enregistrement, même quand on change de caméra.

const KEY = 'sloubi_camfx';
export const DEFAULT_FX = Object.freeze({ light: 0, bright: 100, gray: 0, hue: 0 });
const canFilter = typeof CanvasRenderingContext2D !== 'undefined' && 'filter' in CanvasRenderingContext2D.prototype;

const clamp = (v, lo, hi, d) => (Number.isFinite(+v) ? Math.min(hi, Math.max(lo, +v)) : d);
function loadFx() {
  try {
    const j = JSON.parse(localStorage.getItem(KEY) || '{}');
    return {
      light: clamp(j.light, -50, 50, 0), bright: clamp(j.bright, 50, 150, 100),
      gray: clamp(j.gray, 0, 100, 0), hue: clamp(j.hue, 0, 360, 0),
    };
  } catch { return { ...DEFAULT_FX }; }
}
function saveFx(fx) { try { localStorage.setItem(KEY, JSON.stringify(fx)); } catch { /* navigation privée */ } }
const isDefault = (fx) => Object.keys(DEFAULT_FX).every((k) => fx[k] === DEFAULT_FX[k]);

export async function listCameras() {
  try { return (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'videoinput'); } catch { return []; }
}

export class Camera {
  // constraints : la vidéo demandée (taille, cadence) ; onTrack(piste) : la piste à envoyer a changé
  constructor({ constraints = {}, stable = false, onTrack = () => {}, onEnded = () => {} } = {}) {
    Object.assign(this, { constraints, stable, onTrack, onEnded });
    this.fx = loadFx();
    this.raw = null;       // la piste de la caméra
    this.out = null;       // la piste qu'on envoie (raw, ou celle du canvas)
    this.pipe = null;      // { video, canvas, ctx, track, timer }
    this.switching = false;
    this.count = 0;        // nombre de caméras connues
  }

  get track() { return this.out; }
  get canFilter() { return canFilter; }
  // son propre visage, comme dans un miroir : la caméra de face (ou une webcam, qui ne le dit pas)
  get mirror() { const f = this.raw?.getSettings?.().facingMode; return !f || f === 'user'; }

  async open(deviceId) {
    const video = { ...this.constraints, ...(deviceId ? { deviceId: { exact: deviceId } } : { facingMode: 'user' }) };
    const s = await navigator.mediaDevices.getUserMedia({ video, audio: false });
    const t = s.getVideoTracks()[0];
    t.contentHint = 'motion';
    const old = this.raw;
    this.raw = t;
    t.addEventListener('ended', () => { if (this.raw === t && !this.switching) this.onEnded(); });
    this.count = (await listCameras()).length; // les noms et le nombre ne sont connus qu'après l'autorisation
    if (this.pipe) { // même canvas, même piste envoyée : seule la source change
      this.pipe.video.srcObject = new MediaStream([t]);
      this.pipe.video.play().catch(() => {});
      this.pipe.kick();
    }
    this.route();
    old?.stop();
    return this.out;
  }

  // la caméra suivante (de face ↔ de dos sur un téléphone)
  async next() {
    const cams = await listCameras();
    this.count = cams.length;
    if (cams.length < 2) return false;
    const cur = this.raw?.getSettings?.().deviceId;
    const i = cams.findIndex((c) => c.deviceId === cur);
    this.switching = true;
    try { await this.open(cams[(i + 1) % cams.length].deviceId); } finally { this.switching = false; }
    return true;
  }

  setFx(fx) {
    this.fx = { ...this.fx, ...fx };
    saveFx(this.fx);
    this.route();
  }

  // raw ou canvas, selon les réglages ; prévient si la piste envoyée change
  route() {
    if (!this.raw) return;
    const usePipe = canFilter && (this.stable || !isDefault(this.fx));
    if (usePipe && !this.pipe) this.startPipe();
    if (!usePipe && this.pipe) this.stopPipe();
    const out = usePipe ? this.pipe.track : this.raw;
    if (out !== this.out) {
      this.out = out;
      this.onTrack(out);
    }
  }

  startPipe() {
    const video = document.createElement('video');
    video.muted = true;
    video.playsInline = true;
    video.srcObject = new MediaStream([this.raw]);
    video.play().catch(() => {});
    const canvas = document.createElement('canvas');
    const s = this.raw.getSettings?.() || {};
    canvas.width = s.width || 1280;
    canvas.height = s.height || 720;
    const ctx = canvas.getContext('2d', { alpha: false, desynchronized: true });
    const fps = Math.min(30, s.frameRate || 30);
    const track = canvas.captureStream(fps).getVideoTracks()[0];
    track.contentHint = 'motion';
    const pipe = { video, canvas, ctx, track, timer: 0, alive: true, gen: 0 };
    // une image par image de la caméra quand le navigateur sait le dire, sinon à la cadence voulue
    const schedule = (fn) => {
      if (video.requestVideoFrameCallback) video.requestVideoFrameCallback(fn);
      else pipe.timer = setTimeout(fn, 1000 / fps);
    };
    const draw = (gen) => {
      if (!pipe.alive || gen !== pipe.gen) return; // une source plus récente a pris le relais
      const w = video.videoWidth, h = video.videoHeight;
      if (w && h) {
        if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; } // autre caméra, autre taille
        const { light, bright, gray, hue } = this.fx;
        ctx.filter = `brightness(${bright}%) grayscale(${gray}%) hue-rotate(${hue}deg)`;
        ctx.drawImage(video, 0, 0, w, h);
        ctx.filter = 'none';
        if (light) { // la clarté : un voile blanc ou noir par-dessus
          ctx.fillStyle = light > 0 ? `rgba(255,255,255,${light / 100})` : `rgba(0,0,0,${-light / 100})`;
          ctx.fillRect(0, 0, w, h);
        }
      }
      schedule(() => draw(gen));
    };
    pipe.kick = () => { clearTimeout(pipe.timer); const g = ++pipe.gen; schedule(() => draw(g)); };
    pipe.kick();
    this.pipe = pipe;
  }

  stopPipe() {
    const p = this.pipe;
    if (!p) return;
    p.alive = false;
    clearTimeout(p.timer);
    p.track.stop();
    p.video.srcObject = null;
    this.pipe = null;
  }

  stop() {
    this.stopPipe();
    this.raw?.stop();
    this.raw = this.out = null;
  }
}

/* ========== Les boutons : changer de caméra, régler l'image ========== */
const SLIDERS = [
  ['light', 'Clarté', -50, 50, 1, (v) => `${v > 0 ? '+' : ''}${v}`],
  ['bright', 'Luminosité', 50, 150, 1, (v) => `${v} %`],
  ['gray', 'Noir et blanc', 0, 100, 1, (v) => `${v} %`],
  ['hue', 'Teinte', 0, 360, 1, (v) => `${v}°`],
];
const ICON_SWITCH = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8h3l2-2h6l2 2h3v11H4z"/><path d="M9 13a3 3 0 0 1 5.2-2M15 13a3 3 0 0 1-5.2 2M14 9.5l.4 1.6-1.6.3M10 16.5l-.4-1.6 1.6-.3"/></svg>';
const ICON_TUNE = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2"/><circle cx="10" cy="17" r="2"/></svg>';

// mountControls(hôte, caméra, { onSwitch }) : deux boutons en haut à droite de l'hôte et le panneau des réglages
export function mountControls(host, cam, { onSwitch = () => {} } = {}) {
  const tools = document.createElement('div');
  tools.className = 'cam-tools';
  const mk = (label, icon) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'cam-btn';
    b.setAttribute('aria-label', label);
    b.title = label;
    b.innerHTML = icon; // icônes fixes, écrites ici
    return b;
  };
  const sw = mk('Changer de caméra', ICON_SWITCH);
  const tune = mk('Régler mon image', ICON_TUNE);
  tune.setAttribute('aria-expanded', 'false');
  tune.hidden = !cam.canFilter;
  const panel = document.createElement('div');
  panel.className = 'cam-panel';
  panel.hidden = true;
  panel.setAttribute('role', 'group');
  panel.setAttribute('aria-label', 'Réglages de mon image');
  const inputs = {};
  for (const [k, label, min, max, step, fmt] of SLIDERS) {
    const row = document.createElement('label');
    row.className = 'cam-row';
    const name = document.createElement('span');
    name.textContent = label;
    const out = document.createElement('output');
    const input = Object.assign(document.createElement('input'), { type: 'range', min, max, step, value: cam.fx[k] });
    out.textContent = fmt(cam.fx[k]);
    input.addEventListener('input', () => { out.textContent = fmt(+input.value); cam.setFx({ [k]: +input.value }); });
    inputs[k] = { input, out, fmt };
    row.append(name, out, input);
    panel.appendChild(row);
  }
  const reset = document.createElement('button');
  reset.type = 'button';
  reset.className = 'cam-reset';
  reset.textContent = 'Réinitialiser';
  reset.addEventListener('click', () => {
    cam.setFx({ ...DEFAULT_FX });
    for (const [k, { input, out, fmt }] of Object.entries(inputs)) { input.value = DEFAULT_FX[k]; out.textContent = fmt(DEFAULT_FX[k]); }
  });
  panel.appendChild(reset);
  tools.append(sw, tune);
  host.append(tools, panel);

  const syncSwitch = () => { sw.hidden = cam.count < 2; };
  syncSwitch();
  listCameras().then((c) => { cam.count = Math.max(cam.count, c.length); syncSwitch(); });
  sw.addEventListener('click', async () => {
    sw.disabled = true;
    try { if (await cam.next()) onSwitch(); } catch { /* caméra occupée ou refusée : on garde l'actuelle */ }
    sw.disabled = false;
    syncSwitch();
  });
  tune.addEventListener('click', () => {
    panel.hidden = !panel.hidden;
    tune.setAttribute('aria-expanded', String(!panel.hidden));
  });
  panel.addEventListener('keydown', (e) => { if (e.key === 'Escape') { panel.hidden = true; tune.setAttribute('aria-expanded', 'false'); tune.focus(); } });
  return {
    remove() { tools.remove(); panel.remove(); },
    sync: syncSwitch,
    lockSwitch(on) { sw.disabled = !!on; }, // l'enregistreur sans canvas : pas de changement en pleine prise
    show(on) { tools.hidden = !on; if (!on) panel.hidden = true; },
  };
}
