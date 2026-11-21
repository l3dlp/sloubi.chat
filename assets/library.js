/* library.js — la navigation (des icônes toujours là), la médiathèque et les envois.
   Discussion · Favoris · Images · Vidéos · Documents. Dans un lieu gardé, on
   envoie « à l'arrache » : bouton trombone, glisser-déposer n'importe où sur la
   page, ou coller ; chaque fichier est rangé tout seul par le serveur. Le rendu
   des messages et des fichiers vient d'app.js (window.sloubiFile). */

'use strict';

const CHAN = document.body.dataset.chan;
const API = `/api/${encodeURIComponent(CHAN)}`;
const F = window.sloubiFile; // app.js est chargé avant nous

const nav = document.getElementById('appNav');
const quotaBox = document.getElementById('quota');
const thread = document.getElementById('thread');
const library = document.getElementById('library');
const libTitle = document.getElementById('libraryTitle');
const libSub = document.getElementById('librarySub');
const libTools = document.getElementById('libraryTools');
const libBody = document.getElementById('libraryBody');
const quotaFill = document.getElementById('quotaFill');
const quotaText = document.getElementById('quotaText');

const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
};

/* ========== Les vues ========== */
const VIEWS = {
  favoris: { title: 'Favoris', sub: 'Les messages qui ont reçu un cœur.' },
  image: { title: 'Images', sub: 'Toutes les photos et images envoyées ici.' },
  video: { title: 'Vidéos', sub: 'Les vidéos envoyées ici, et celles partagées en lien (YouTube, Vimeo, Dailymotion).' },
  document: { title: 'Documents', sub: 'PDF, sons, fichiers à télécharger, et les liens partagés.' },
};
let view = 'chat';

function show(next) {
  view = next;
  nav.querySelectorAll('.nav-item[data-view]').forEach((b) => {
    const on = b.dataset.view === next;
    b.classList.toggle('is-active', on);
    if (on) b.setAttribute('aria-current', 'page'); else b.removeAttribute('aria-current');
  });
  if (quotaBox) quotaBox.hidden = next === 'chat' || next === 'favoris';
  if (next === 'chat') {
    library.hidden = true;
    thread.hidden = false;
    document.getElementById('chatInput')?.focus();
    return;
  }
  thread.hidden = true;
  library.hidden = false;
  libTitle.textContent = VIEWS[next].title;
  libSub.textContent = VIEWS[next].sub;
  libTools.replaceChildren();
  libBody.replaceChildren(empty('Un instant…'));
  if (next === 'favoris') loadFavorites('all');
  else loadFiles(next);
}

// seulement les vues : l'icône « Post-it » de la même barre est gérée par app.js
nav.querySelectorAll('.nav-item[data-view]').forEach((b) => b.addEventListener('click', () => show(b.dataset.view)));
document.getElementById('libraryBack').addEventListener('click', () => show('chat'));

function empty(text) {
  const p = document.createElement('p');
  p.className = 'empty';
  p.textContent = text;
  return p;
}

/* ---------- Favoris ---------- */
function loadSet(k) { try { const a = JSON.parse(store.get(k) || '[]'); return Array.isArray(a) ? a : []; } catch { return []; } }

async function loadFavorites(which) {
  // deux filtres : tous les messages aimés du salon, ou seulement ceux que j'ai aimés (gardés dans ce navigateur)
  libTools.replaceChildren(...[['all', 'Les plus aimés'], ['mine', 'Mes cœurs']].map(([k, label]) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'chip';
    b.textContent = label;
    b.setAttribute('aria-pressed', String(k === which));
    b.addEventListener('click', () => loadFavorites(k));
    return b;
  }));
  let q = '';
  if (which === 'mine') {
    const ids = loadSet(`sloubi_liked_${CHAN}`).slice(-200);
    if (!ids.length) { libBody.replaceChildren(empty('Tu n’as encore mis de cœur sur aucun message. Clique sur ♡ sous un message, ou double-clique dessus.')); return; }
    q = `?ids=${ids.join(',')}`;
  }
  try {
    const r = await fetch(`${API}/favorites${q}`, { cache: 'no-store' });
    if (!r.ok) throw new Error(String(r.status));
    const { messages } = await r.json();
    if (view !== 'favoris') return;
    if (!messages.length) { libBody.replaceChildren(empty('Aucun cœur pour l’instant. Le premier qui aime un message lancera la liste.')); return; }
    const ul = document.createElement('ul');
    ul.className = 'fav-list';
    for (const m of messages) {
      const li = document.createElement('li');
      li.className = 'fav';
      const head = document.createElement('div');
      head.className = 'fav-head';
      const who = document.createElement('span');
      who.textContent = `${m.nickname} · ${new Date(m.timestamp).toLocaleString('fr-FR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}`;
      const hearts = document.createElement('span');
      hearts.textContent = m.likes ? `♥ ${m.likes}` : '♡';
      head.append(who, hearts);
      const body = document.createElement('div');
      if (m.file) body.appendChild(F.fileBlock(m.file));
      else F.fillText(body, m.message || '', m);
      li.append(head, body);
      ul.appendChild(li);
    }
    libBody.replaceChildren(ul);
  } catch {
    libBody.replaceChildren(empty('Impossible de charger les favoris. Réessaie.'));
  }
}

/* ---------- Images, vidéos, documents ---------- */
let gallery = []; // ce qu'affiche la visionneuse (images de la vue en cours)

async function loadFiles(kind, before = 0) {
  try {
    const r = await fetch(`${API}/files?kind=${kind}${before ? `&before=${before}` : ''}`, { cache: 'no-store' });
    if (!r.ok) throw new Error(String(r.status));
    const j = await r.json();
    if (view !== kind) return;
    paintStats(j);
    const files = j.files || [];
    const links = j.links || [];
    if (!before) {
      gallery = [];
      if (!files.length && !links.length) {
        libBody.replaceChildren(empty({
          image: 'Pas encore d’image. Glisse des photos sur la page, ou clique sur le trombone à côté du message.',
          video: 'Pas encore de vidéo. Glisse une vidéo sur la page pour la partager.',
          document: 'Pas encore de document. Glisse un PDF ou n’importe quel fichier sur la page.',
        }[kind]));
        return;
      }
      libBody.replaceChildren();
      if (files.length) {
        if (links.length) libBody.appendChild(section(kind === 'video' ? 'Envoyées ici' : 'Fichiers'));
        libBody.appendChild(kind === 'document' ? Object.assign(document.createElement('ul'), { className: 'doc-list' })
          : Object.assign(document.createElement('div'), { className: 'gallery' }));
      }
      if (links.length) {
        libBody.appendChild(section(kind === 'video' ? 'En lien' : 'Liens'));
        const list = document.createElement(kind === 'video' ? 'div' : 'ul');
        list.className = kind === 'video' ? 'vlinks' : 'weblinks';
        for (const l of links) list.appendChild(kind === 'video' ? videoCard(l) : webRow(l));
        libBody.appendChild(list);
      }
    }
    const box = libBody.querySelector('.gallery, .doc-list');
    if (!box) return;
    for (const f of files) box.appendChild(kind === 'document' ? docRow(f) : tile(f));
    libBody.querySelector('.more')?.remove();
    if (files.length === 60) { // il y en a d'autres : un bouton plutôt qu'un défilement infini, plus simple à comprendre
      const more = document.createElement('button');
      more.type = 'button';
      more.className = 'chip more';
      more.textContent = 'Voir les plus anciens';
      more.addEventListener('click', () => loadFiles(kind, files[files.length - 1].messageId));
      libBody.appendChild(more);
    }
  } catch {
    libBody.replaceChildren(empty('Impossible de charger cette page. Réessaie.'));
  }
}

function tile(f) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'tile';
  b.title = `${f.name} · ${f.nickname}`;
  b.setAttribute('aria-label', `${f.kind === 'video' ? 'Vidéo' : 'Image'} ${f.name}, envoyée par ${f.nickname}`);
  if (f.kind === 'video') {
    const v = document.createElement('video');
    v.preload = 'metadata';
    v.muted = true;
    v.playsInline = true;
    v.src = `${F.fileURL(f)}#t=0.1`;
    const play = document.createElement('span');
    play.className = 'tile-play';
    play.textContent = '▶';
    b.append(v, play);
  } else {
    const img = document.createElement('img');
    img.loading = 'lazy';
    img.decoding = 'async';
    img.alt = '';
    img.src = F.fileURL(f, f.thumb ? '?thumb=1' : '');
    b.appendChild(img);
  }
  gallery.push(f);
  const index = gallery.length - 1;
  b.addEventListener('click', () => openViewer(index));
  return b;
}

function section(text) {
  const h = document.createElement('h3');
  h.className = 'lib-section';
  h.textContent = text;
  return h;
}

const PROVIDERS = { youtube: 'YouTube', vimeo: 'Vimeo', dailymotion: 'Dailymotion' };
const when = (ts) => new Date(ts).toLocaleDateString('fr-FR', { day: 'numeric', month: 'short' });

// une vidéo partagée en lien : sa miniature (gardée par notre serveur), son titre, et elle s'ouvre ailleurs
function videoCard(l) {
  const a = document.createElement('a');
  a.className = 'vlink';
  a.href = l.url;
  a.target = '_blank';
  a.rel = 'noopener noreferrer nofollow ugc';
  const pic = document.createElement('span');
  pic.className = `vlink-thumb vlink-${l.provider}`;
  const img = document.createElement('img');
  img.loading = 'lazy';
  img.decoding = 'async';
  img.alt = '';
  img.src = `${API}/vthumb/${l.provider}/${encodeURIComponent(l.id)}`;
  img.addEventListener('error', () => img.remove(), { once: true }); // pas de miniature : la couleur de la plateforme
  const play = document.createElement('span');
  play.className = 'vlink-play';
  play.setAttribute('aria-hidden', 'true');
  play.textContent = '▶';
  pic.append(img, play);
  const meta = document.createElement('span');
  meta.className = 'vlink-meta';
  const t = document.createElement('b');
  t.textContent = l.title || `Vidéo ${PROVIDERS[l.provider] || ''}`;
  const sub = document.createElement('small');
  const author = l.author && l.author !== PROVIDERS[l.provider] ? ` · ${l.author}` : '';
  sub.textContent = `${PROVIDERS[l.provider]}${author} · ${l.nickname}, ${when(l.timestamp)}`;
  meta.append(t, sub);
  a.append(pic, meta);
  a.addEventListener('click', () => F.countClick(l.messageId, l.i));
  return a;
}

// un lien qui n'est pas une vidéo : son site, son adresse, qui l'a partagé et combien de clics
function webRow(l) {
  const li = document.createElement('li');
  const a = document.createElement('a');
  a.className = 'file-doc weblink';
  a.href = l.url;
  a.target = '_blank';
  a.rel = 'noopener noreferrer nofollow ugc';
  const badge = document.createElement('span');
  badge.className = 'file-ext';
  badge.textContent = (l.host.replace(/^www\./, '')[0] || '?').toUpperCase();
  const txt = document.createElement('span');
  const host = document.createElement('span');
  host.className = 'file-name';
  host.textContent = l.host;
  const path = document.createElement('span');
  path.className = 'file-size';
  path.textContent = F.prettyUrl(l.url);
  txt.append(host, path);
  const meta = document.createElement('span');
  meta.className = 'doc-meta';
  meta.textContent = `${l.nickname}\n${when(l.timestamp)}${l.clicks ? `\n↗ ${l.clicks} ${l.clicks > 1 ? 'clics' : 'clic'}` : ''}`;
  a.append(badge, txt, meta);
  a.addEventListener('click', () => F.countClick(l.messageId, l.i));
  li.appendChild(a);
  return li;
}

function docRow(f) {
  const li = document.createElement('li');
  const card = F.docCard(f);
  const meta = document.createElement('span');
  meta.className = 'doc-meta';
  meta.textContent = `${f.nickname}\n${new Date(f.timestamp).toLocaleDateString('fr-FR')}`;
  card.appendChild(meta);
  li.appendChild(card);
  if (f.kind === 'audio') li.appendChild(F.fileBlock(f)); // un son : on peut l'écouter sur place
  return li;
}

/* ---------- Combien, et combien de place ---------- */
function paintStats(j) {
  if (j.counts) {
    for (const [k, n] of Object.entries(j.counts)) {
      const el = nav.querySelector(`.nav-count[data-count="${k}"]`);
      if (el) el.textContent = n ? String(n) : '';
    }
  }
  if (quotaText && j.quota) {
    const pct = Math.min(100, (j.used / j.quota) * 100);
    quotaFill.style.width = `${pct.toFixed(1)}%`;
    quotaText.textContent = `${F.humanSize(j.used)} utilisés sur ${F.humanSize(j.quota)}`;
  }
}

async function refreshStats() {
  try {
    const r = await fetch(`${API}/files`, { cache: 'no-store' });
    if (r.ok) paintStats(await r.json());
  } catch { /* plus tard */ }
}

/* ========== La visionneuse ========== */
const box = document.getElementById('lightbox');
const stage = document.getElementById('lightboxStage');
const caption = document.getElementById('lightboxCaption');
const dl = document.getElementById('lightboxDl');
let at = 0;
let opener = null;

function openViewer(i) {
  opener = document.activeElement;
  at = i;
  paintViewer();
  box.hidden = false;
  document.getElementById('lightboxClose').focus();
}

function paintViewer() {
  const f = gallery[at];
  if (!f) return;
  let el;
  if (f.kind === 'video') {
    el = document.createElement('video');
    el.controls = true;
    el.autoplay = true;
    el.playsInline = true;
    el.src = F.fileURL(f);
  } else {
    el = document.createElement('img');
    el.alt = f.name;
    el.src = F.fileURL(f);
  }
  stage.replaceChildren(el);
  caption.textContent = `${f.name}${f.nickname ? ` · ${f.nickname}` : ''}${gallery.length > 1 ? ` · ${at + 1} / ${gallery.length}` : ''}`;
  dl.href = F.fileURL(f, '?dl=1');
  dl.setAttribute('download', f.name);
  document.getElementById('lightboxPrev').hidden = gallery.length < 2;
  document.getElementById('lightboxNext').hidden = gallery.length < 2;
}

function closeViewer() {
  box.hidden = true;
  stage.replaceChildren();
  opener?.focus();
}

const step = (d) => { at = (at + d + gallery.length) % gallery.length; paintViewer(); };
document.getElementById('lightboxClose').addEventListener('click', closeViewer);
document.getElementById('lightboxPrev').addEventListener('click', () => step(-1));
document.getElementById('lightboxNext').addEventListener('click', () => step(1));
box.addEventListener('click', (e) => { if (e.target === box) closeViewer(); });
box.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeViewer();
  if (e.key === 'ArrowLeft') step(-1);
  if (e.key === 'ArrowRight') step(1);
});
// glisser du doigt pour passer à la suivante
let touchX = null;
box.addEventListener('touchstart', (e) => { touchX = e.touches[0].clientX; }, { passive: true });
box.addEventListener('touchend', (e) => {
  if (touchX === null) return;
  const dx = e.changedTouches[0].clientX - touchX;
  if (Math.abs(dx) > 60 && gallery.length > 1) step(dx < 0 ? 1 : -1);
  touchX = null;
});

// une image cliquée dans le fil : on l'ouvre seule
window.addEventListener('sloubi:view', (e) => {
  gallery = [e.detail];
  openViewer(0);
});

/* ========== Les envois (lieu gardé seulement) ========== */
const fileInput = document.getElementById('fileInput');
const trayEl = document.getElementById('uploads');
const dropZone = document.getElementById('dropZone');

if (fileInput && trayEl) {
  const queue = [];
  let running = 0;
  const PARALLEL = 3;
  const nickname = () => store.get('sloubi_nickname') || '';
  const clientId = () => store.get('sloubi_client_id') || '';

  document.getElementById('attachButton').addEventListener('click', () => fileInput.click());
  fileInput.addEventListener('change', () => {
    enqueue([...fileInput.files]);
    fileInput.value = '';
  });

  // glisser-déposer n'importe où sur la page
  let depth = 0;
  const hasFiles = (e) => [...(e.dataTransfer?.types || [])].includes('Files');
  window.addEventListener('dragenter', (e) => { if (!hasFiles(e)) return; depth++; dropZone.hidden = false; });
  window.addEventListener('dragleave', (e) => { if (!hasFiles(e)) return; if (--depth <= 0) { depth = 0; dropZone.hidden = true; } });
  window.addEventListener('dragover', (e) => { if (hasFiles(e)) e.preventDefault(); });
  window.addEventListener('drop', (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    dropZone.hidden = true;
    enqueue([...e.dataTransfer.files]);
  });

  // coller une capture d'écran ou un fichier copié
  window.addEventListener('paste', (e) => {
    const files = [...(e.clipboardData?.files || [])];
    if (!files.length) return;
    e.preventDefault();
    enqueue(files.map((f, i) => (f.name && f.name !== 'image.png' ? f
      : new File([f], `collé-${new Date().toISOString().slice(0, 19).replace(/[T:]/g, '-')}${i ? `-${i}` : ''}.${(f.type.split('/')[1] || 'bin').replace('jpeg', 'jpg')}`, { type: f.type }))));
  });

  window.sloubiUpload = (files) => enqueue(files); // pour recorder.js : un enregistrement part comme un fichier

  function enqueue(files) {
    if (!files.length) return;
    if (!nickname()) {
      window.dispatchEvent(new CustomEvent('sloubi:need-nick'));
      return;
    }
    for (const file of files) {
      if (!file.size) continue; // un dossier glissé arrive comme un fichier vide
      queue.push({ file, row: trayRow(file) });
    }
    trayEl.hidden = false;
    pump();
  }

  function trayRow(file) {
    const li = document.createElement('li');
    li.className = 'upload';
    const name = document.createElement('span');
    name.className = 'upload-name';
    name.textContent = file.name;
    const st = document.createElement('span');
    st.className = 'upload-state';
    st.textContent = `${F.humanSize(file.size)} · en attente`;
    const bar = document.createElement('span');
    bar.className = 'upload-bar';
    bar.appendChild(document.createElement('i'));
    li.append(name, st, bar);
    trayEl.appendChild(li);
    return li;
  }

  function pump() {
    while (running < PARALLEL && queue.length) {
      running++;
      send(queue.shift()).finally(() => { running--; pump(); tidy(); });
    }
  }

  // XHR plutôt que fetch : lui seul donne la progression d'un envoi
  function send({ file, row }) {
    return new Promise((resolve) => {
      const st = row.querySelector('.upload-state');
      const fill = row.querySelector('.upload-bar i');
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `${API}/files`);
      xhr.setRequestHeader('X-File-Name', encodeURIComponent(file.name || 'fichier'));
      xhr.setRequestHeader('X-Nickname', encodeURIComponent(nickname()));
      xhr.setRequestHeader('X-Client-Id', encodeURIComponent(clientId()));
      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return;
        const pct = (e.loaded / e.total) * 100;
        fill.style.width = `${pct.toFixed(1)}%`;
        st.textContent = `${Math.round(pct)} %`;
      };
      xhr.onload = () => {
        if (xhr.status === 201) {
          row.classList.add('done');
          fill.style.width = '100%';
          st.textContent = 'Envoyé ✓';
          try { window.dispatchEvent(new CustomEvent('sloubi:sent', { detail: JSON.parse(xhr.responseText) })); } catch {}
        } else {
          row.classList.add('failed');
          st.textContent = {
            403: 'refusé : il faut le mot de passe du salon',
            413: 'refusé : l’espace du salon (10 Go) est plein',
            507: 'refusé : le serveur n’a plus de place',
            429: 'trop d’envois d’un coup, réessaie',
            400: 'envoi incomplet',
          }[xhr.status] || `échec (${xhr.status})`;
        }
        resolve();
      };
      xhr.onerror = () => { row.classList.add('failed'); st.textContent = 'connexion coupée'; resolve(); };
      st.textContent = '0 %';
      xhr.send(file);
    });
  }

  // les envois réussis s'effacent après un moment ; les échecs restent pour qu'on les voie
  function tidy() {
    if (running || queue.length) return;
    setTimeout(() => {
      trayEl.querySelectorAll('.upload.done').forEach((li) => li.remove());
      trayEl.hidden = !trayEl.children.length;
      refreshStats();
    }, 2500);
  }
}

// un fichier arrive dans le fil : les compteurs du menu suivent
window.addEventListener('sloubi:message', (e) => {
  if (e.detail?.live && e.detail.file && quotaText) refreshStats();
});
if (quotaText) refreshStats();
