# API

Toutes les routes sont déclarées dans `src/web/routes.go` (lignes 169 à 212). Les `POST` JSON exigent `Content-Type: application/json` et un corps de 4 Ko au plus (sauf l’envoi de fichier, en corps brut).

**Salon verrouillé** : toutes les routes `/api/<chan>/…` et `/ws/<chan>` passent par `server.enter` et répondent `403` sans le cookie d’accès `sloubi_k_<chan>`. Exceptions : `check`, `info` et `unlock`, qui servent justement à entrer.

## Pages

| Méthode | Route | Rôle |
|---|---|---|
| `GET` | `/` | Accueil et landing (`views/home.html`). |
| `GET` | `/_/` | Registre des salons (`views/directory.html`), `noindex`, cache 15 s, 2000 salons au plus. Aucun salon ne peut commencer par `_`. |
| `GET` | `/<chan>` | Page du salon (`views/room.html`). Majuscules → 301 vers les minuscules ; `/<chan>/` → `/<chan>` ; nom invalide ou interdit → 404 qui rend l’accueil avec le nom en rouge. |
| `GET` | `/sw.js` | Service worker des notifications, à la racine pour avoir la portée `/`. |
| `GET` | `/assets/…` | Fichiers statiques. |

## Salon

| Méthode | Route | Entrée | Sortie |
|---|---|---|---|
| `GET` | `/api/check/<chan>` | — | `{name, ok, reason, exists, theme, locked, access}` ; `reason` ∈ `invalid`, `toolong`, `forbidden`. Limité à 4/s avec ban de 10 min après 30 refus. |
| `GET` | `/api/<chan>/info` | en-tête `X-Owner-Key` facultatif, session facultative | `{exists, theme, locked, access, owner, claimable, signedIn, member, pending, status?}`, plus `{messages, lastId, sloubi}` si l’on a accès. |
| `POST` | `/api/<chan>/unlock` | `{password}` | Pose le cookie d’accès (HttpOnly, 30 jours). Essais limités (`lockLimit` : ban de 15 min après 10 échecs). |
| `POST` | `/api/<chan>/settings` | `{ownerKey, theme?, password?, removePassword?}` + session | Sur un salon inexistant : le fonde et renvoie `ownerKey` (et le rattache au compte connecté). Thèmes : `taverne`, `foret`, `moulin`, `cyber`, `kawaii`, `bd`. Mot de passe de 4 à 128 caractères : sans session `401 login required` ; membre pas encore validé → `{pending:true}` et le lieu reste ouvert ; `removePassword` annule aussi une demande. Droits : voir `canSettle`. Thème ou mot de passe changé → `Evict` du salon avant la réponse. |
| `GET` | `/api/<chan>/microbiotes` | — | Les `#mots` du salon et leur nombre. |
| `GET` | `/api/<chan>/microbiotes/<mot>` | — | L’encyclopédie : les 100 derniers messages qui citent le mot. |

## Domaines perso

un lieu peut s’ouvrir sur son propre nom de domaine. Le créateur (membre validé ou admin) l’écrit dans les options du lieu ; le domaine doit pointer sur le serveur. Sur ce domaine, `/` est le lieu ; l’API, le WebSocket et les fichiers statiques passent ; `/<lieu>` ramène à `/` ; tout autre chemin repart vers `https://sloubi.chat`. Un domaine qui n’est rattaché à aucun lieu renvoie vers `https://sloubi.chat`. Le proxy peut demander `GET /_/domaine?domain=…` (en local seulement) avant de prendre un certificat : 200 pour un domaine rattaché, 404 sinon.

Réglage : `POST /api/<chan>/settings {domain}` (`""` détache) ; `401` sans session, `403` si l’on n’est pas membre validé, `400` domaine invalide (IP, localhost, sloubi.chat ou ses sous-domaines, accents non convertis en punycode), `409` déjà pris par un autre lieu. `info` renvoie `domain`.

## Comptes

Le préfixe `_` ne peut pas commencer un nom de salon : pas de collision avec `/api/<chan>/…`.

| Méthode | Route | Entrée | Sortie |
|---|---|---|---|
| `GET` | `/_/compte` | — | La page « Mon compte ». `?retour=/<salon>` y ramène après la connexion. |
| `POST` | `/api/_compte/code` | `{email}` | `{ok}` ; `400` adresse invalide ; `429` renvoi trop tôt (`sent:true` : le code précédent vaut toujours) ou trop de demandes par IP ; `502` l’e-mail n’est pas parti. |
| `POST` | `/api/_compte/verify` | `{email, code}` | `{ok, email, role, status}` + cookie `sloubi_session` ; crée le compte au besoin. `401` avec la raison (pas de code, expiré, trop d’essais, mauvais code). |
| `GET` | `/api/_compte/me` | session | `{signedIn:false}` ou `{signedIn, email, role, status, rooms:[{name, theme, locked, pending}]}` |
| `POST` | `/api/_compte/logout` | — | efface la session |
| `GET` | `/api/_admin/users` | session admin | `{users:[{id, email, role, status, createdAt, requestedAt, rooms}]}`, demandes en attente d’abord. `403` sinon. |
| `POST` | `/api/_admin/users/<id>` | `{action: "valider" \| "refuser" \| "retirer"}` | `{ok, locked:[lieux fermés]}`. L’admin lui-même n’est pas concerné. |

## Messages et réactions

| Méthode | Route | Entrée | Sortie |
|---|---|---|---|
| `POST` | `/api/<chan>/messages` | `{clientId, nickname, message}` | `202 {"status":"ok","id":…}` (+ `ownerKey` si le message fonde le salon). Sans pseudo valide : `400 {"error":"nickname required"}`. 500 caractères au plus. |
| `GET` | `/api/<chan>/last-sloubi` | — | `{number, exists, nickname, message, timestamp}` |
| `POST` | `/api/nick` | `{nickname}` | `{ok, nickname, reason}` ; `reason` ∈ `tooshort`, `toolong`, `chars`, `forbidden`. |
| `POST` | `/api/<chan>/postit` | `{clientId, text}` | Le post-it (280 caractères), diffusé au salon. |
| `POST` | `/api/<chan>/like` | `{id, delta: 1 \| -1}` | Nouveau total ; jamais sous zéro. Diffuse `{type:"like", id, likes}`. |
| `POST` | `/api/<chan>/click` | `{id, i}` | `i` = rang du lien dans le message. Diffuse `{type:"click", id, i, clicks}`. |

## Fichiers (salons verrouillés seulement)

| Méthode | Route | Rôle |
|---|---|---|
| `POST` | `/api/<chan>/files` | Corps brut = le fichier. En-têtes `X-File-Name`, `X-Nickname`, `X-Client-Id`, encodés en URL. Classe, fait la miniature, poste un message portant `file`. Erreurs : quota du salon (10 Go), disque du serveur (moins de 5 Go libres), pas de pseudo, salon non verrouillé. |
| `GET` | `/api/<chan>/files[?kind=image\|video\|audio\|document]` | Liste, comptes par type, usage et quota ; avec les liens partagés (vidéos et liens web). |
| `GET` | `/api/<chan>/files/<id>[?thumb=1][&dl=1]` | Le fichier. Inline seulement pour les médias sûrs, sinon en pièce jointe ; CSP `sandbox` toujours. |
| `GET` | `/api/<chan>/favorites[?ids=1,2,3]` | Les messages les plus aimés, ou ceux dont on donne les ids (« mes cœurs »). |
| `GET` | `/api/<chan>/vthumb/<provider>/<id>` | Miniature d’une vidéo YouTube / Vimeo / Dailymotion **citée dans ce salon**, servie depuis le cache du serveur. |

## Appels et notifications

| Méthode | Route | Rôle |
|---|---|---|
| `GET` | `/api/<chan>/turn` | `{iceServers}` : STUN, plus TURN avec identifiants de 6 h si le serveur a un secret. Salon verrouillé et accès requis. |
| `GET` | `/api/push-key` | Clé publique VAPID (base64url). |
| `POST` | `/api/<chan>/push` | `{endpoint, keys:{p256dh, auth}, tag}` pour s’abonner ; `{endpoint, remove:true}` pour se désabonner. Hôtes de push connus seulement. |

## WebSocket `/ws/<chan>`

Même origine seulement. À la connexion : les 100 derniers messages, le post-it, puis le direct. Les clients dédoublonnent par `id`.

**Serveur → client**

| `type` | Contenu |
|---|---|
| `message` | champs du message à plat : `id`, `channel`, `nickname`, `message`, `timestamp`, `sloubiNumber`, `likes`, `clicks`, `file` |
| `postit` | `channel`, `text`, `updatedAt` |
| `like`, `click` | nouveaux totaux |
| `room` | `{theme, locked}` : le décor ou la serrure a changé : le serveur ferme le socket juste après, le client recharge |
| `call` | `joined`, `full`, `denied`, `nick-required`, `stage-busy`, `handoff`, `handoff-declined`, `signal`, `roster` (avec `live`) |

**Client → serveur** (32 Ko par trame, débit limité par client)

| Trame | Rôle |
|---|---|
| `{type:"hello", push}` | l’étiquette de mon abonnement push, pour ne pas me sonner si je suis déjà là |
| `{type:"call", op:"join", nick, listen}` | entrer dans l’appel (`listen` : spectateur sans micro) |
| `{type:"call", op:"leave"}` | sortir |
| `{type:"call", op:"signal", to, data}` | SDP / ICE pour un pair |
| `{type:"call", op:"state", muted}` | micro coupé ou non |
| `{type:"call", op:"live", on}` | prendre ou rendre la scène |
| `{type:"call", op:"handoff", to}` | proposer la scène à quelqu’un |
| `{type:"call", op:"handoff-decline"}` | refuser la proposition reçue |

## Anciennes routes

`POST /verif.json`, `GET /last-sloubi.json` et `GET /ws` sont des alias du salon `chante`, gardés pour les PWA encore en cache.
