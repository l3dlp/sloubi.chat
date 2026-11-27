# Architecture

Un binaire Go (Gin + SQLite pur Go) sert des pages HTML rendues par `html/template`, une API JSON, un WebSocket par salon et des fichiers. Le front est en JavaScript sans étape de build (modules ES natifs), avec Three.js auto-hébergé pour les scènes 3D.

```
navigateur ──HTTPS──▶ Caddy ──▶ 127.0.0.1:8888 sloubi (Go)
     │                                 ├─ db/chat.sqlite   (WAL)
     │                                 ├─ db/uploads/<chan>/<id>[.thumb]
     │                                 └─ db/vthumbs/<provider>-<id>
     ├──WebRTC (maillage, ≤ 6)──▶ autres navigateurs
     └──TURN──▶ coturn de BeamCam (beamcam.live:3478, même machine)
```

## Pile

| Couche | Choix |
|---|---|
| Langage | Go 1.27.3 |
| HTTP | Gin (mode release), `gin-contrib/gzip` |
| WebSocket | `gophre/websocket` (API à la gorilla) |
| Base | `modernc.org/sqlite` (pur Go, pas de CGO), mode WAL |
| Texte | `golang.org/x/text` (NFC, catégories Unicode) |
| Crypto | bibliothèque standard uniquement : `crypto/pbkdf2`, `crypto/hkdf`, `crypto/ecdh`, `crypto/ecdsa`, `crypto/hmac`, `crypto/sha1` |
| Front | JS vanilla en modules, Three.js (`assets/vendor/three/`, MIT), canvas 2D, WebAudio, WebRTC, MediaRecorder |
| Polices | auto-hébergées (`assets/fonts/`, OFL) : Permanent Marker, Archivo, et celles des thèmes |

Le serveur résout `assets/`, `views/`, `db/` et `forbidden.json` **depuis le répertoire courant**.

## Serveur (`src/web/`)

### `routes.go`
- `server` : `store`, `hub`, `forbidden`, un `Limiter` par action (voir [SECURITE.md](SECURITE.md#limites-de-débit)), `uploads reservations`, `videos *videoFetcher`, `turn turnConfig`, `push *pusher`, le cache de l’annuaire (`dir*`, 15 s).
- `Run` : ouvre la base, `newPusher(store)`, `newVideoFetcher()`, lance `store.reclassify()` en tâche de fond, déclare les routes, puis `http.Server` avec des délais courts (en-têtes 5 s, lecture 15 s, écriture 30 s).
- Middlewares, dans l’ordre : `gin.Logger`, `gin.Recovery`, `securityHeaders`, `floodGuard` (plafond global par IP), gzip (sauf `/ws` et `/api/<chan>/files…`).
- Hygiène des saisies : `application/json` exigé, corps de 4 Ko au plus, `cleanText` (NFC, retrait des caractères de contrôle et de format sauf ZWJ, 2 diacritiques par lettre au plus).
- `channelResolver` : les anciennes routes (`/verif.json`, `/last-sloubi.json`, `/ws`) réutilisent les mêmes handlers, fixés sur `chante`.
- `channelPage` passe au gabarit `Chan`, `Theme`, `Themes` (`themeList()`), `Locked` (pas encore de cookie d’accès) et `Calls` (salon verrouillé **et** accès accordé).

### `rooms.go` : lieux, créateur, mot de passe
- Thèmes : `taverne`, `foret`, `moulin`, `cyber`, `kawaii`, `bd`. `themes` (libellés, couleur, script FX) et `themeOrder` (ordre des cartes dans les réglages), `themeList()`, `themeOf`.
- `Room{Exists, Theme, OwnerHash, PwHash}`.
- **Créateur** : `newOwnerKey()` aléatoire, donnée **une seule fois** dans la réponse qui fonde le salon ; seul son SHA-256 est gardé (`channels.owner_hash`). Le client la garde dans `localStorage` (`sloubi_owner_<chan>`) ou l’adopte depuis un lien `/<chan>#cle=<clé>`.
- **Mot de passe** : `hashPassword` en PBKDF2-SHA256, 210 000 itérations, sel aléatoire (`channels.pw_hash`).
- **Accès** : cookie `sloubi_k_<chan>` = HMAC(`meta.cookie_secret`, chan + pw_hash), HttpOnly, 30 jours. Changer le mot de passe change le hachage, donc révoque tous les cookies.
- `server.enter` : passage obligé de toutes les routes de salon et du WS ; 403 sans accès.
- `settings` : un thème ou un mot de passe qui change appelle `hub.Evict(chan)` **avant** de répondre (événement `{type:"room"}` puis fermeture des sockets ; les clients rechargent).
- `microbiotes` : les `#mots` d’un salon (table `tags`) et l’encyclopédie d’un mot. `extractTags` (Go) et `TAG_RE` (`app.js`) doivent rester identiques.
- `cleanNickname` : NFC, alphabet latin + chiffres ASCII + espace `_ - . '`, 2 à 24 caractères, `forbidden.json` appliqué, « Anonyme » réservé. Les caractères invalides sont refusés, pas retirés.

### `db.go` : `Store`
- `Store.wmu` sérialise les écritures sensibles. La règle du sloubi (`insertMessage`) : `sloubi <N>` avec N = max + 1 du salon, et un `clientId` différent de celui du sloubi validé précédent ; vérification et insertion dans une transaction, adossées à `UNIQUE(channel, sloubi_number)`. Seulement dans les salons `taverne`.
- `lastMessages` : les 100 derniers, joints à `likes` et `files`, puis `fillClicks`.
- `listChannels` : pour `/_/`, avec `Theme` et `Locked` (les chiffres des salons verrouillés sont masqués).
- WAL reporté dans le fichier principal au démarrage puis toutes les heures.

#### Schéma

| Table | Colonnes | Rôle |
|---|---|---|
| `messages` | `id`, `client_id`, `nickname`, `message`, `ts`, `sloubi_number`, `channel`, `file_id` | le fil ; `channel` ajouté en v2 (défaut `chante`), `file_id` en v4 |
| `channels` | `name`, `created_at`, `last_message_at`, `theme`, `owner_hash`, `pw_hash`, `owner_user`, `pw_pending`, `domain` | un salon existe dès son premier message, post-it ou réglage ; `owner_user` et `pw_pending` en v5 (comptes) |
| `users` | `id`, `email`, `role`, `status`, `created_at`, `requested_at` | comptes (admin unique, membres) |
| `otp_codes` | `email`, `code_hash`, `issued_at`, `expires_at`, `attempts` | codes de connexion en attente |
| `postits` | `channel`, `text`, `updated_at`, `updated_by` | un post-it par salon |
| `meta` | `key`, `value` | `cookie_secret`, `vapid_private`, `admin_claimed` |
| `tags` | salon, mot, message | les microbiotes (remplie rétroactivement à sa création) |
| `files` | `id`, `channel`, `name`, `mime`, `kind`, `size`, `w`, `h`, `thumb`, `created_at` | fichiers partagés |
| `likes` | `message_id`, `n` | total de cœurs |
| `link_clicks` | `message_id`, `idx`, `n` | total de clics par lien (`idx` = rang du lien dans le message) |
| `video_meta` | `provider`, `vid`, `title`, `author`, `thumb`, `fetched_at` | cache oEmbed |
| `push_subs` | `endpoint`, `channel`, `p256dh`, `auth`, `tag`, `created_at` | abonnements Web Push |

Index : `(channel, id)`, `ts`, unique `(channel, sloubi_number)`, `files(channel, kind)`.

Les migrations sont idempotentes (`hasColumn`, `CREATE … IF NOT EXISTS`) et tournent à chaque démarrage.

### `message.go` : le hub
- `Hub` : salon → clients. Chaque `client` a un canal `send` tamponné et **un seul** `writePump` qui écrit sur la connexion. Un tampon plein déconnecte le client lent.
- `client` : `pid`, `nick`, `inCall`, `muted`, `listen`, `tag`, et un seau à jetons pour ce qu’il envoie.
- `join` pousse l’historique et le post-it sous le verrou du hub : les clients dédoublonnent par `id`.
- `readPump(h, onMessage)` : trames entrantes de 32 Ko au plus (`maxSignalBytes`), remises à `server.onClientMessage`.
- `Evict(chan)`, `sendLocked`.

### `accounts.go` et `mail.go` : comptes (novembre 2026)
- Discuter ne demande aucun compte. Le compte sert à **garder un lieu par mot de passe**, réservé à l'admin et aux membres validés.
- Connexion par code e-mail à 6 chiffres, reprise de Daily Jungle (`/intelligences/apps/dailyjungle.net`, `pkg/auth` et `pkg/mail`) mais stockée en SQLite : table `otp_codes` (code haché en HMAC, 10 min, renvoi après 60 s, 5 essais). `issueCode`, `checkCode`.
- Session : cookie `sloubi_session` = base64(`uid:exp`) + HMAC(`meta.cookie_secret`), HttpOnly, SameSite=Lax, 90 jours. `server.user(c)` relit le compte en base à chaque requête : une décision de l'admin s'applique tout de suite.
- Envoi (`mail.go`, interface `codeMailer`) : SMTP (`SMTP_USER` / `SMTP_PASS`, Gmail par défaut) → API Brevo (`BREVO_API_KEY`) → journal du service (dev).
- Rôles : `admin` (unique, l'adresse `ADMIN_EMAIL`, posé par `ensureAdmin` au démarrage) et `membre`. Statuts : `aucun`, `en-attente`, `valide`, `refuse`.
- `ensureAdmin` : la première fois (`meta.admin_claimed`), l'admin devient `owner_user` de **tous les lieux existants**. Une autre adresse rétrograde l'ancienne : un seul admin.
- `canSettle(r, clé, user)` : l'admin règle tout ; un lieu rattaché à un compte (`owner_user`) ne se règle que par ce compte (la clé ne compte plus) ; sinon la clé de créateur.
- Mot de passe :
  - sans session → `401 login required` ;
  - admin ou membre `valide` → `pw_hash` tout de suite (`Store.lock`) ;
  - sinon → **demande** (`Store.requestLock`) : hachage dans `channels.pw_pending`, compte rattaché au lieu, statut `en-attente`, le lieu reste ouvert.
- Décision de l'admin (`Store.decide`) : `valider` passe `pw_pending` en `pw_hash` et évince ces lieux ; `refuser` efface les demandes ; `retirer` remet le statut à `aucun` mais **laisse fermés** les lieux déjà gardés (les rouvrir publierait leurs conversations).
- Handlers : `authCode`, `authVerify`, `authLogout`, `authMe`, `adminUsers`, `adminDecide`, `accountPage`.

### `domains.go` : domaines perso
- `channels.domain` (index unique, v6). `cleanDomain` (nom ASCII ou punycode, jamais un hôte du site ni un de ses sous-domaines), `setDomain`, `channelByDomain`.
- `loadSite` : `SITE_URL` (défaut https://sloubi.chat) et ses hôtes (+ `www.`, + `SITE_HOSTS`). Les IP et localhost comptent comme le site.
- `hostGate` (middleware, avant gzip) : site → rien ne change ; domaine connu → `/` rend le lieu (`renderRoom(c, chan, true)`), `/api/`, `/ws/`, `/assets/`, `/sw.js`, manifeste et icônes passent, `/<lieu>` → `/`, le reste → le site ; domaine inconnu → `302` vers `SITE_URL/`.
- `renderRoom` passe `Home`, `Account` et `Base` (`data-base` : `/` sur un domaine perso, `/<lieu>` sinon) ; `app.js` construit ses liens avec `BASE`.
- `domainAsk` : `GET /_/domaine?domain=` pour le TLS à la demande du proxy, clients locaux seulement (`ClientIP` en loopback : une requête relayée par Caddy porte l’IP réelle).
- Les cookies (accès, session) sont liés à l’hôte : sur un domaine perso, on redonne le mot de passe.

### `icons.go` : l'application d'un domaine perso
- Sur son domaine, le lieu s'installe comme sa propre PWA : `/manifest.json` porte son nom (`appName` : « tribu » → « Tribu »), `start_url` et `scope` à `/`, les couleurs de son décor.
- Icônes `/icon-192.png`, `/icon-512.png`, `/apple-touch-icon.png` (180), `/favicon.png` et `/favicon.ico` (64) : l'initiale en capitale, police Go Bold (`golang.org/x/image`, BSD), couleur d'accent du décor (`accent`, les `--ember` de `themes.css`) sur sa couleur de fond, plein bord (« maskable »). Dessinées à la demande, gardées en mémoire, `Cache-Control` d'une heure.
- La page passe `Title`, `AppName`, `Icon`, `Touch` ; sur sloubi.chat rien ne change (« Sloubi », icônes d'origine).

### `calls.go` : appels de groupe
- Seulement dans les salons verrouillés, pour les clients passés par `enter`. `maxCallers = 6`.
- Opérations reçues sur le WS (`{type:"call", op}`) : `join` (avec `listen` pour un spectateur), `leave`, `signal` (relayé au seul `to`, entre participants), `state` (micro coupé), `live` (prendre ou rendre la scène), `handoff`, `handoff-decline`. Plus `{type:"hello", push}` pour l’étiquette d’abonnement.
- Réponses : `joined` (pairs existants : c’est le nouveau venu qui fait les offres, elles ne se croisent jamais), `full`, `denied`, `nick-required`, `stage-busy`, `handoff`, `handoff-declined`, `signal`, `roster` (à tout le salon, avec `live`).
- État : `Hub.calls` (salon → participants), `Hub.stage` (le présentateur), `Hub.relay` (le relais proposé). Quitter l’appel rend la scène ; perdre le socket quitte l’appel.
- `turnCreds` : identifiants coturn REST (`<expiration>:<pid>`, HMAC-SHA1, 6 h). `loadTurn` lit `TURN_SECRET` ou `TURN_SECRET_FILE`, `TURN_HOST` (défaut `beamcam.live`), `TURN_PORT` (3478). Sans secret : STUN seul.
- Premier `join` d’un appel → `started` + les `tag` des navigateurs présents → `notifyCall` pousse aux autres.

### `push.go` : Web Push sans dépendance
- `encryptPush` : RFC 8291 (`aes128gcm`, ECDH P-256 + HKDF). `vapidAuth` : JWT RFC 8292 signé ES256, clé privée dans `meta.vapid_private`.
- `pushHostAllowed` : fcm.googleapis.com, android.googleapis.com, *.push.services.mozilla.com, *.notify.windows.com, web.push.apple.com, *.push.apple.com.
- `notifyCall` : une fois toutes les 90 s par salon au plus, TTL 120 s, 300 abonnements par salon au plus ; les points d’accès qui répondent 404/410 sont oubliés.

### `reactions.go` : liens, cœurs, clics
- `linkRe` / `extractLinks` (20 liens au plus) doivent correspondre à `LINK_RE` dans `app.js` : le rang du lien est l’index de clic.
- `like` (±1, jamais sous zéro), `click`, `fillClicks`. Diffusion `{type:"like"|"click"}`.

### `files.go` : fichiers
- `roomQuota = 1001 Go` (le vrai plafond est le disque, en attendant le CDN), `diskReserve = 5 Go`, `thumbMax = 640 px`, `thumbMaxPix = 50 Mpx`, `maxNameRunes = 120`. `uploadRoot = db/uploads` (variable : les tests la remplacent).
- `uploadFile` : corps brut, en-têtes `X-File-Name`, `X-Nickname`, `X-Client-Id` encodés en URL ; écrit en flux dans `db/uploads/<chan>/<id aléatoire>` ; `reservations` empêche des envois parallèles de dépasser le quota ; `diskFree` (`syscall.Statfs`) refuse sous 5 Go libres ; `longRequest` prolonge les délais via `http.ResponseController`.
- `classify` : d’abord les octets (`http.DetectContentType`, `inlineMime`), puis l’extension (`extMime`). HTML et SVG sont toujours des documents. Un son en conteneur vidéo reste un son grâce à l’extension (`audioExt` : `.weba`, `.m4a`, …).
- Miniatures JPEG pour jpeg/png/gif : orientation EXIF appliquée (`jpegOrientation`, `exifOrientation`, `orient2`), transparence posée sur du blanc.
- `serveFile` : inline seulement pour les médias de la liste blanche, sinon en pièce jointe ; toujours `Content-Security-Policy: sandbox`. `?thumb=1`, `?dl=1`.
- `listFilesRoute` (comptes par type, usage, liens), `favorites`, `reclassify` (reclasse au démarrage les fichiers mal rangés par une ancienne version).
- `privateRoom` : les fichiers n’existent que dans les salons verrouillés.

### `videolinks.go` : liens dans la médiathèque
- `videoOf` : YouTube (watch, shorts, live, embed, youtu.be), Vimeo, Dailymotion ; `canonicalVideo`.
- `sharedLinks` : parcourt les messages du salon ; vidéos → `VideoLink`, autres liens http(s) → `WebLink` (avec clics).
- `videoFetcher` : oEmbed + URL de miniature fixe, hôtes en liste blanche (`videoHosts`), redirections comprises ; cache dans `db/vthumbs` (`vthumbRoot`) et `video_meta` ; nouvel essai après 24 h ; miniature 3 Mo max, oEmbed 64 Ko max.
- `videoThumb` ne sert qu’une vidéo **citée dans ce salon** (`citesVideo`) : pas de proxy ouvert. `fillVideoMeta` attend 4 s au plus.

### Autres
- `channels.go` : regex des noms `^[a-z0-9]+([._-][a-z0-9]+)*$` (32 max), `forbidden.json` (normalisation, listes `exact` / `allow` / `contains`, rechargement à chaud).
- `limiter.go` : seau à jetons par clé (IPv4, ou /64 en IPv6), bannissement temporaire optionnel après N refus.
- `websocket.go` : upgrader, même origine seulement.
- `src/chat/message.go` : `Message` (`ClientID` en `json:"-"`, `Likes`, `Clicks map[string]int`, `File *FileRef`), `Postit`, `FileRef{ID, Name, Mime, Kind, Size, W, H, Thumb}`.

## Client (`assets/`)

| Fichier | Rôle |
|---|---|
| `viewport.js` | chargé en premier : `--vvh` / `--vvt` depuis `visualViewport`, classe `kb-open`, événement `sloubi:viewport` |
| `app.js` | page de salon : thème, porte (mot de passe), pseudo obligatoire (`nickMode`), WebSocket, rendu des messages (bulles, avatars, jours, cœurs, fichiers), liens, post-it, réglages du créateur (`place`, `runButton`, `postChange`, `reloadDelay`), score (`COUNTERS`), ambiance |
| `library.js` | barre d’icônes (`#appNav`, `.nav-item[data-view]`), vues Favoris / Images / Vidéos / Documents, jauge de quota, visionneuse, envois (XHR, 3 en parallèle, glisser-déposer, coller) ; expose `window.sloubiUpload` |
| `recorder.js` | enregistreur maison du « + » : message vocal, vidéo, écran + micro ; `MediaRecorder`, relecture, envoi via `sloubiUpload` |
| `camfx.js` | sa caméra pour le live et l’enregistreur : changer de caméra, régler son image (canvas + `ctx.filter`) |
| `call.js` | appels, live, relais, spectateurs, cloche Web Push ; parle au socket par événements |
| `ring.js` | sonnerie FM générative (WebAudio) |
| `sw.js` | service worker servi à `/sw.js` : notifications seulement, **aucun** gestionnaire `fetch` |
| `fx.js` | choisit le décor : la scène du thème sur ordinateur, `shader-bg.js` sur écran tactile |
| `shader-bg.js` | fond des téléphones : un shader sans boucle à 1/6 de la résolution, 20 i/s |
| `room-fx.js` | taverne (Three.js, autonome) |
| `fx-kit.js` | socle des scènes 3D : rendu, boucle, dégradation, mouvement réduit, `daylight()`, `skyColor()`, `body[data-daylight]` |
| `forest-fx.js`, `mill-fx.js` | forêt et moulin (sur `fx-kit.js`) |
| `fx2d.js` | socle des scènes 2D (cadence image par image via `fps`) |
| `cyber-fx.js`, `kawaii-fx.js`, `bd-fx.js` | les trois thèmes 2D ; ils habillent aussi le fil via `detail.el` de `sloubi:message` |
| `home.js` | accueil : validation locale, `/api/check` avec délai, mot de passe dès l’accueil |
| `login.js` | connexion par code e-mail (`mountLogin`, `me`, `logout`), partagée par `/_/compte` et les options du lieu |
| `account.js` + `account.css` | `/_/compte` : connexion, mes lieux, et pour l’admin les demandes (Valider / Refuser / Retirer l’accès) |
| `room.css`, `themes.css`, `home.css`, `marker.css`, `directory.css` | styles ; `themes.css` recolore `room.css` par `body[data-theme]` |

### Événements entre scripts (sur `window`)

| Événement | Émis par | Écouté par | Contenu |
|---|---|---|---|
| `sloubi:message` | `app.js` | scènes | `detail.el`, `detail.live` (faux pour l’historique), `detail.tags` |
| `sloubi:count`, `sloubi:milestone` | `app.js` | scènes | compteur, palier tous les 10 messages |
| `sloubi:send` | `call.js` | `app.js` | trame à envoyer sur le WS |
| `sloubi:call` | `app.js` | `call.js` | trame d’appel reçue |
| `sloubi:ws-open`, `sloubi:ws-closed` | `app.js` | `call.js` | perdre le socket raccroche |
| `sloubi:viewport` | `viewport.js` | `app.js` | garder le fil collé en bas |

### Stockage local du navigateur

| Clé | Contenu |
|---|---|
| `sloubi_owner_<chan>` | clé de créateur |
| `sloubi_mine_<chan>` | ids de mes messages (le serveur ne dit jamais qui a écrit quoi) |
| `sloubi_liked_<chan>` | mes cœurs |
| `sloubi_postit_<chan>` | position et état du post-it |
| pseudo, `clientId`, Mode Triche | réglages du visiteur |

Tout accès est dans un `try/catch` : navigation privée et stockage bloqué doivent marcher.

### Vues (`views/`)
- `room.html` : barre du haut (marque, salon, score `#score`, Options), `nav#appNav` (dont `#navPostit`), `main.room-main` → `section.thread` (scène live, fil, porte, `.room-bottom` : appel, envois, saisie avec `#attachButton` et `#recordButton` + `#recordMenu`) et `section.library` (`#quota`). Fenêtres : `#recorder`, `#lightbox`, `#dropZone`, post-it, réglages. Scripts : `viewport.js`, `app.js`, `library.js`, `recorder.js` (si `Calls`), la scène, `call.js` (si `Calls`).
- `home.html` : l’accueil, qui sert aussi de landing.
- `directory.html` : `/_/`, le registre des salons.
- `account.html` : `/_/compte`, mon compte (style de `home.css`).

Aucun style ni script inline (CSP) : les données serveur passent par des attributs `data-*`, l’affichage par l’attribut `hidden` ou le CSSOM.

## Flux

**Message.** `POST /api/<chan>/messages` → `enter` → nettoyage → `insertMessage` (transaction, sloubi, tags) → diffusion WS à tout le salon → chaque client rend la bulle (à droite si l’id est dans `sloubi_mine_<chan>`). La réponse donne l’`id` (et `ownerKey` si le message fonde le salon).

**Fichier.** `POST /api/<chan>/files` (corps brut) → réservation de quota → écriture en flux → `classify` → miniature → ligne `files` → message avec `file` diffusé. La médiathèque relit `GET /api/<chan>/files?kind=`.

**Appel.** `GET /api/<chan>/turn` → `join` sur le WS → `joined` avec les pairs → le nouveau venu crée une `RTCPeerConnection` par pair, avec un transceiver vidéo vide en `sendrecv` → `signal` relayés → audio de tous vers tous. Live : `op:"live"` réserve la scène, puis `replaceTrack()` sur les émetteurs vidéo existants (aucune renégociation).

**Push.** Le navigateur s’abonne (`/api/push-key`, puis `POST /api/<chan>/push` avec un `tag`), annonce son `tag` par `hello` ; au premier `join`, le serveur pousse à tous les abonnés du salon sauf ceux déjà présents.
