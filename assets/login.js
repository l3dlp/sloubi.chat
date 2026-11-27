// login.js — la connexion par code e-mail (comme Daily Jungle) : l'adresse, puis
// le code à 6 chiffres reçu. Sert à la page /_/compte et aux options d'un lieu.
// mountLogin(racine, { onDone, inputClass, buttonClass }) : onDone(compte) une fois connecté.

const API = '/api/_compte';

async function call(path, body) {
  const r = await fetch(`${API}/${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
  const j = await r.json().catch(() => ({}));
  return { status: r.status, ok: r.ok, ...j };
}

function el(tag, props = {}, ...kids) {
  const e = Object.assign(document.createElement(tag), props);
  e.append(...kids);
  return e;
}

export function mountLogin(root, { onDone, inputClass = '', buttonClass = '' } = {}) {
  const email = el('input', {
    type: 'email', className: `otp-input ${inputClass}`, autocomplete: 'email', inputMode: 'email',
    placeholder: 'ton@adresse.fr', maxLength: 254, required: true, spellcheck: false, enterKeyHint: 'send',
  });
  email.setAttribute('aria-label', 'Ton adresse e-mail');
  const send = el('button', { type: 'button', className: `otp-btn ${buttonClass}`, textContent: 'Recevoir un code' });
  const code = el('input', {
    type: 'text', className: `otp-input otp-code ${inputClass}`, autocomplete: 'one-time-code', inputMode: 'numeric',
    placeholder: '123456', maxLength: 6, pattern: '[0-9]*', enterKeyHint: 'go',
  });
  code.setAttribute('aria-label', 'Le code reçu par e-mail');
  const enter = el('button', { type: 'button', className: `otp-btn ${buttonClass}`, textContent: 'Entrer' });
  const back = el('button', { type: 'button', className: 'otp-link', textContent: 'Changer d’adresse ou renvoyer' });
  const note = el('p', { className: 'otp-note' });
  note.setAttribute('aria-live', 'polite');

  const stepEmail = el('div', { className: 'otp-step' }, el('div', { className: 'otp-row' }, email, send));
  const sentTo = el('b');
  const stepCode = el('div', { className: 'otp-step' },
    el('p', { className: 'otp-sent' }, 'Code envoyé à ', sentTo, '. Il est valable 10 minutes.'),
    el('div', { className: 'otp-row' }, code, enter), back);
  stepCode.hidden = true;
  root.replaceChildren(el('div', { className: 'otp' }, stepEmail, stepCode, note));

  const say = (t, bad) => { note.textContent = t; note.classList.toggle('is-bad', !!bad); };
  const busy = (b, on, label) => { b.disabled = on; if (label) b.textContent = label; };

  function toCode() {
    sentTo.textContent = email.value.trim();
    stepEmail.hidden = true;
    stepCode.hidden = false;
    code.value = '';
    code.focus();
  }

  async function askCode() {
    const addr = email.value.trim();
    if (!email.checkValidity() || !addr) { say('Il faut une adresse e-mail valable.', true); email.focus(); return; }
    busy(send, true, 'Envoi…');
    say('');
    try {
      const j = await call('code', { email: addr });
      // 429 avec sent : un code vient déjà de partir, on peut le taper
      if (j.ok || j.sent) { toCode(); if (j.sent) say(j.error); return; }
      say(j.error || 'Ça n’a pas marché, réessaie.', true);
    } catch {
      say('Le serveur ne répond pas.', true);
    } finally {
      busy(send, false, 'Recevoir un code');
    }
  }

  let checking = false;
  async function check() {
    if (checking) return; // Entrée et le 6e chiffre arrivent parfois ensemble
    const v = code.value.replace(/\D/g, '');
    if (v.length !== 6) { say('Le code a 6 chiffres.', true); code.focus(); return; }
    checking = true;
    busy(enter, true, 'Vérification…');
    say('');
    try {
      const j = await call('verify', { email: email.value.trim(), code: v });
      if (j.ok) { say('Connecté ✓'); enter.textContent = 'Connecté ✓'; onDone?.(j); return; }
      say(j.error || 'Ce n’est pas le bon code.', true);
      code.select();
    } catch {
      say('Le serveur ne répond pas.', true);
    }
    checking = false;
    busy(enter, false, 'Entrer');
  }

  send.addEventListener('click', askCode);
  enter.addEventListener('click', check);
  email.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); askCode(); } });
  code.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); check(); } });
  code.addEventListener('input', () => {
    code.value = code.value.replace(/\D/g, '').slice(0, 6);
    if (code.value.length === 6) check(); // un code collé ou proposé par le clavier : on entre tout de suite
  });
  back.addEventListener('click', () => { stepCode.hidden = true; stepEmail.hidden = false; say(''); email.focus(); });

  return { focus: () => email.focus() };
}

export async function me() {
  try {
    const r = await fetch(`${API}/me`, { cache: 'no-store' });
    return r.ok ? await r.json() : { signedIn: false };
  } catch {
    return { signedIn: false, offline: true };
  }
}

export async function logout() {
  await fetch(`${API}/logout`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{}' }).catch(() => {});
}
