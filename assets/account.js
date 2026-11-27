// account.js — /_/compte : se connecter (code e-mail), voir ses lieux, et pour
// l'admin, valider ou refuser les demandes de mot de passe.
import { mountLogin, me, logout } from './login.js';

const $ = (id) => document.getElementById(id);
const title = $('acctTitle');
const lead = $('acctLead');
const LEAD = lead.textContent;

// ?retour=/salon : après la connexion, on y revient (un nom de salon seulement, pas une URL ailleurs)
const back = (() => {
  const r = new URLSearchParams(location.search).get('retour') || '';
  return /^\/[a-z0-9]+([._-][a-z0-9]+)*$/.test(r) && r.length <= 33 ? r : '';
})();

const STATUS = {
  aucun: 'Tu peux discuter partout sans compte. Pour garder un lieu par mot de passe, pose-le dans les options du lieu\u00a0: ça envoie une demande, que l’admin valide.',
  'en-attente': 'Ta demande est en attente\u00a0: l’admin va la regarder. D’ici là, tes lieux restent ouverts, et ton mot de passe s’activera tout seul.',
  valide: 'Tu es membre\u00a0: tu peux garder tes lieux par mot de passe.',
  refuse: 'Ta demande n’a pas été acceptée. Tes lieux restent ouverts.',
};

function el(tag, props = {}, ...kids) {
  const e = Object.assign(document.createElement(tag), props);
  e.append(...kids);
  return e;
}

const fmtDate = (iso) => {
  const d = new Date(iso);
  return isNaN(d) ? '' : d.toLocaleDateString('fr-FR', { day: 'numeric', month: 'long', year: 'numeric' });
};

function roomLine(r) {
  const state = r.locked ? '🔒 gardé' : r.pending ? '⏳ mot de passe en attente' : 'ouvert';
  return el('li', { className: 'acct-room' },
    el('a', { href: `/${r.name}`, textContent: `/${r.name}` }),
    el('span', { className: `acct-state${r.pending ? ' is-pending' : ''}`, textContent: state }));
}

async function render() {
  const acct = await me();
  if (acct.offline) { lead.textContent = 'Le serveur ne répond pas. Réessaie dans un instant.'; return; }
  $('logout').hidden = !acct.signedIn;
  $('mine').hidden = !acct.signedIn;
  if (!acct.signedIn) {
    title.textContent = 'Connexion';
    lead.textContent = LEAD;
    $('loginBox').hidden = false;
    mountLogin($('loginBox'), {
      onDone: () => {
        if (back) { location.href = back; return; }
        $('loginBox').hidden = true;
        render();
      },
    }).focus();
    return;
  }
  $('loginBox').hidden = true;
  title.textContent = acct.email;
  lead.textContent = acct.role === 'admin' ? 'Tu es l’admin\u00a0: tes lieux sont gardés quand tu veux, et c’est toi qui valides les autres.' : (STATUS[acct.status] || '');
  $('myRooms').replaceChildren(...(acct.rooms || []).map(roomLine));
  $('myRoomsEmpty').hidden = (acct.rooms || []).length > 0;
  if (back) { $('back').hidden = false; $('backLink').href = back; $('backLink').textContent = `Revenir à ${back}`; }
  if (acct.role === 'admin') { $('admin').hidden = false; loadUsers(); }
}

const ACTIONS = {
  aucun: [],
  'en-attente': [['valider', 'Valider'], ['refuser', 'Refuser']],
  valide: [['retirer', 'Retirer l’accès']],
  refuse: [['valider', 'Valider quand même']],
};
const LABEL = { aucun: 'connecté·e, rien demandé', 'en-attente': 'demande en attente', valide: 'membre', refuse: 'refusé·e' };

async function decide(u, action, btn) {
  if (action === 'retirer' && !confirm(`Retirer l’accès de ${u.email} ? Ses lieux déjà gardés le restent ; il ou elle ne pourra plus en garder d’autres.`)) return;
  btn.disabled = true;
  btn.textContent = 'En cours…';
  try {
    const r = await fetch(`/api/_admin/users/${u.id}`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ action }),
    });
    if (!r.ok) throw new Error();
  } catch {
    btn.textContent = 'Échec, réessaie';
    btn.disabled = false;
    return;
  }
  loadUsers();
}

async function loadUsers() {
  let users = [];
  try {
    const r = await fetch('/api/_admin/users', { cache: 'no-store' });
    if (!r.ok) return;
    users = (await r.json()).users || [];
  } catch { return; }
  const others = users.filter((u) => u.role !== 'admin');
  $('usersEmpty').hidden = others.length > 0;
  $('users').replaceChildren(...others.map((u) => {
    const acts = (ACTIONS[u.status] || []).map(([a, label]) => {
      const b = el('button', { type: 'button', className: `acct-btn${a === 'valider' ? ' is-main' : ''}`, textContent: label });
      b.addEventListener('click', () => decide(u, a, b));
      return b;
    });
    const when = u.status === 'en-attente' && u.requestedAt ? ` · depuis le ${fmtDate(u.requestedAt)}` : ` · inscrit·e le ${fmtDate(u.createdAt)}`;
    return el('li', { className: `acct-user is-${u.status}` },
      el('div', { className: 'acct-user-head' },
        el('b', { textContent: u.email }),
        el('span', { className: 'acct-state', textContent: (LABEL[u.status] || u.status) + when })),
      u.rooms.length ? el('ul', { className: 'acct-rooms' }, ...u.rooms.map(roomLine)) : '',
      acts.length ? el('div', { className: 'acct-actions' }, ...acts) : '');
  }));
}

$('logout').addEventListener('click', async () => { await logout(); location.reload(); });
render();
