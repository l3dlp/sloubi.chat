# Sloubi.chat — le chat médiéval indépendant

Meta a fermé les discussions de groupe ?  
On ne va pas pleurer, on va forger notre propre taverne.

**Sloubi** est une réponse directe à la fermeture des groupes de discussion par Meta.  
Plutôt que de se laisser enfermer dans des walled gardens qui tirent la prise quand ça les arrange, on s’est construit un **chat indépendant, libre et fun**, centré autour d’un seul mot magique : **sloubi**.

Ici, pas d’algos, pas de pub, pas de flicage. Juste un compteur communautaire qui monte à chaque sloubi validé.

---

## Fonctionnalités

- **Ton salon en une URL** : sur l’accueil, tape un nom, tu as ton `sloubi.chat/<nom>`. L’accueil dit tout de suite si le nom est libre, existe déjà (et dans quel décor), ou est refusé et pourquoi. Pour un salon gardé, le mot de passe se donne là, et l’on arrive déverrouillé. La page présente aussi les lieux, les usages, l’histoire du nom et la licence.  
  Règle : `a-z`, `0-9`, et `.` `_` `-` jamais doublés ni en bord, 32 caractères max.  
- **La taverne historique** vit sur [`/chante`](https://sloubi.chat/chante), en hommage à *Perceval chante Sloubi* (Kaamelott, livre III) ; un lien direct depuis l’accueil.  
- **Chaque salon est une taverne enfumée.** Une scène 3D (Three.js) où dérivent des volutes éclairées par l’âtre, des rais de lumière, des braises, des chopes de bière et des dés qui flottent. Les messages se condensent hors de la fumée et s’y dissolvent en montant ; chaque sloubi validé fait jaillir une gerbe d’étincelles du compteur. La scène s’allège toute seule sur une machine lente, se fige avec `prefers-reduced-motion`, et laisse place à un fond peint sans WebGL.  
- **`/nick <pseudo>`** pour changer de pseudo (aussi dans les paramètres). Le serveur le valide et renvoie sa forme canonique : alphabet latin, chiffres, espace et `_ - . '`, 2 à 24 caractères, Unicode normalisé (NFC), noms réservés refusés. Pas de sosies cyrilliques, de caractères invisibles ni de zalgo.  
- **Six lieux au choix**, chacun avec son décor et son petit jeu. Le créateur les change d’un clic dans les paramètres, et c’est immédiat pour tout le monde :  
  - **la taverne** (par défaut) : on compte les sloubis ;  
  - **la forêt nourricière** : un sous-bois au crépuscule, des lucioles, des microbiotes qui luisent au milieu de leurs planches en cercle. Chaque message est une graine semée ; tous les dix, un arbre prend racine. Un `#mot` dans un message fait pousser un microbiote : on clique dessus et s’ouvre son encyclopédie, tout ce qui a été dit sous ce mot ;  
  - **cyberpunk 1984**, cousin du punk : un fanzine photocopié (collages, papiers déchirés, grain de photocopieuse, grille fluo, bandes VHS). Chaque message colle un de ses mots au mur et se « déchiffre » à l’arrivée ; les pseudos deviennent des lettres anonymes ; un narrateur `SYS>` commente tout ;  
  - **kawaii** : un ciel pastel où tout a des yeux, nuages et mochis compris. Le bouton envoyer est un personnage : il suit le curseur, lit ce qu’on écrit, s’endort si on l’ignore, sourit quand on envoie. Tous les dix messages, un arc-en-ciel ;  
  - **BD franco-belge** : une planche en ligne claire. Les messages sont des bulles, le système parle en cartouches (« Pendant ce temps… »), une onomatopée (BOUM, PAF, VLAN…) surgit en grand ; toutes les six cases, on tourne la page ;  
  - **le moulin au bord de la rivière** : l’eau qui file et reflète le moulin, la roue, les arbres et le ciel ; la roue qui tourne. Chaque message met à l’eau une barque de papier avec sa bougie et donne un tour de roue ; tous les dix tours, la meule remplit un sac de farine.  
- **La forêt et le moulin suivent l’heure du visiteur** : aube brumeuse, plein jour, couchant, puis la nuit, ses étoiles, les fenêtres du meunier et les lucioles. `?heure=14` dans l’URL force une heure, pour essayer.  
- **Le créateur d’un lieu** est celui qui le fonde (premier message, premier post-it, ou les paramètres d’un lieu encore vide). Il reçoit une clé secrète gardée dans son navigateur, et peut copier un **lien de créateur** (`/<chan>#cle=…`) pour régler son lieu depuis un autre appareil. Les lieux fondés avant cette fonction, `/chante` compris, n’ont pas de créateur.  
- **Mot de passe** (optionnel, posé par le créateur) : sans lui, on voit la porte et le décor, rien d’autre. Changer le mot de passe met tout le monde dehors ; le retirer rouvre le lieu.  
- **Domaine perso** : un lieu peut s’ouvrir sur son propre nom de domaine. Le créateur (membre validé ou admin) l’écrit dans les options du lieu ; le domaine doit pointer sur le serveur. Sur ce domaine, `/` est le lieu ; l’API, le WebSocket et les fichiers statiques passent ; `/<lieu>` ramène à `/` ; tout autre chemin repart vers `https://sloubi.chat`. Un domaine qui n’est rattaché à aucun lieu renvoie vers `https://sloubi.chat`. Le proxy peut demander `GET /_/domaine?domain=…` (en local seulement) avant de prendre un certificat : 200 pour un domaine rattaché, 404 sinon.  
- **Mot de passe réservé aux membres** : discuter ne demande aucun compte, mais garder un lieu (et donc ses fichiers, ses vocaux, ses appels) est réservé à l’admin et aux membres validés. On se connecte par un **code reçu par e-mail** (comme sur Daily Jungle : rien à retenir), sur [`/_/compte`](https://sloubi.chat/_/compte) ou directement dans les options du lieu. Le premier mot de passe posé est une **demande** : son hachage attend, le lieu reste ouvert, et il s’active tout seul quand l’admin valide le membre. L’admin ne voit jamais les mots de passe. L’admin est unique (`ADMIN_EMAIL`) et devient, à sa création, propriétaire de tous les lieux existants.  
- **Appels de groupe dans les lieux gardés** : celles et ceux qui ont le mot de passe peuvent s’appeler, jusqu’à 6 à la fois. Bouton « Lancer l’appel » au-dessus de la saisie ; chacun voit qui est en ligne, qui parle (la pastille s’allume) et qui a coupé son micro. **On peut rejoindre sans micro**, en simple spectateur. **Quand quelqu’un lance l’appel, ça sonne** chez les présents : une petite mélodie douce, tirée au hasard à chaque appel (une patch de synthé modulaire générée en direct). La cloche 🔔 abonne aux **notifications** (Web Push) : on est prévenu même onglet fermé (sur iPhone, seulement si le site est installé sur l’écran d’accueil). **Live, un par un** : quand la scène est libre, quelqu’un de l’appel passe en live avec sa caméra (ou son écran, sur ordinateur) et les autres le regardent en grand ; quand il ou elle rend la scène, la suivante peut la prendre. En live, un clic sur la pastille de quelqu’un qui a un micro **lui passe le relais** : il accepte en un clic (caméra ou écran), et l’on descend de scène. Le son va directement de navigateur à navigateur (WebRTC, maillage) ; le serveur ne fait que relayer la mise en relation par le WebSocket du salon, donc l’appel est exactement aussi privé que le lieu. Relais TURN : le coturn de [BeamCam](https://beamcam.live), dont Sloubi reprend aussi les réglages audio et la relance ICE (MIT).  
- **Une vraie messagerie** : le fil dans un panneau bien visible, les bulles de chacun (les tiennes à droite), l’heure, les jours. Les liens sont cliquables et s’ouvrent dans un nouvel onglet ; on peut mettre un cœur sur n’importe quel message (double-clic). On ne compte rien d’autre que des totaux : clics par lien, cœurs par message.  
- **Une barre d’icônes toujours visible** (à gauche en paysage, en haut en portrait) : Discussion, Favoris (les plus aimés, ou mes cœurs), Post-it, et dans un salon gardé Images, Vidéos, Documents, avec le nombre de fichiers en pastille.  
- **Messages vocaux, vidéos et partage d’écran** dans les salons gardés : le « + » à côté du trombone ouvre l’enregistreur maison (message vocal, vidéo, ou « Mon écran + micro » sur ordinateur, pour montrer quelque chose sans la caméra) ; on réécoute ou on revoit avant d’envoyer, et le navigateur compresse (une minute de voix ≈ 240 Ko, une minute de vidéo 720p ≈ 9 Mo).  
- **Les liens rangés aussi** : une vidéo YouTube, Vimeo ou Dailymotion partagée en lien rejoint les Vidéos avec sa miniature et son titre (récupérés et gardés par le serveur : les navigateurs ne contactent pas ces plateformes) ; les autres liens rejoignent les Documents, avec leur total de clics. Tout s’ouvre dans un nouvel onglet.  
- **Partage de fichiers** dans les salons gardés : jusqu’à 1001 Go par salon (le serveur garde toujours 5 Go libres ; les fichiers passeront sur un CDN avant l’ouverture). On envoie à l’arrache (trombone, glisser-déposer n’importe où, coller) et tout se range tout seul d’après le contenu ; miniatures pour les photos, visionneuse, téléchargement.  
- **Chat en temps réel** par salon : REST pour poster, WebSocket pour recevoir.  
- **Sloubi compteur** dans les tavernes, arbitré par le serveur :  
  - il faut écrire `sloubi <N>` (casse indifférente, ex. `Sloubi 42`) où **N est exactement le prochain numéro** ;  
  - on ne peut pas valider deux sloubis d’affilée (le même `clientId` que le dernier sloubi validé est refusé) ;  
  - un message qui ne remplit pas ces conditions est publié normalement, sans compter.  
- **Post-it partagé** sur chaque salon : 280 caractères, modifiable par tous, mis à jour en direct. Déplaçable, refermable en punaise ; position propre à chaque visiteur.  
- **Pseudo obligatoire** avant d’écrire ou d’appeler : tant qu’on n’en a pas, le champ de saisie le demande. Gardé dans le `localStorage` du navigateur ; « Anonyme » est réservé.  
- **Mode Triche** (réglages) : affiche un bouton raccourci ; la validation reste côté serveur.  
- **Ambiance** : messages d’ambiance aléatoires côté client, propres à chaque lieu.  
- **PWA installable** : manifest et icônes.  

---

## Structure

La documentation détaillée (architecture, API, sécurité, exploitation, pièges, historique, à faire) est dans [`docs/`](docs/README.md).

```
sloubi.chat/
├─ main.go              # point d’entrée (lit PORT, défaut 8888)
├─ go.mod / go.sum      # Go 1.27.3, Gin, gophre/websocket, modernc SQLite (pur Go)
├─ forbidden.json       # noms de salon interdits (rechargé à chaud ; non publié, voir forbidden.example.json)
├─ bin/build.sh         # build + govulncheck + redémarrage du service
├─ src/
│  ├─ chat/message.go   # Message et Postit (JSON partagé REST / DB / WS)
│  └─ web/
│     ├─ routes.go      # routes HTTP + WS, nettoyage des saisies
│     ├─ rooms.go       # les trois lieux, créateur, mot de passe, microbiotes
│     ├─ db.go          # SQLite, migrations, règle de validation du sloubi
│     ├─ channels.go    # règle des noms de salon + forbidden.json
│     ├─ limiter.go     # rate-limit par IP
│     ├─ message.go     # Hub WebSocket par salon
│     └─ websocket.go   # upgrader WS (même origine)
├─ views/
│  ├─ home.html         # accueil : le gros /________
│  ├─ directory.html    # /_/ : le registre des tavernes
│  └─ room.html         # page d’un salon, quel que soit son décor
├─ assets/              # app.js (chat), room-fx.js / forest-fx.js / mill-fx.js (scènes 3D, les deux dernières sur fx-kit.js),
│                       # room.css + themes.css, home.js, fonts/, vendor/three/, manifest, icônes
└─ db/chat.sqlite       # base SQLite (auto-créée, ignorée par git)
```

---

## Lancer

### Depuis une release

Chaque [release GitHub](https://github.com/l3dlp/sloubi.chat/releases) fournit une archive par plateforme (Linux amd64 et arm64) : le binaire, `assets/`, `views/`, `deploy/` et `forbidden.example.json`. Aucune dépendance : Go compile tout, SQLite compris.

```
tar xzf sloubi-v1.0.0-linux-amd64.tar.gz && cd sloubi-v1.0.0-linux-amd64
cp forbidden.example.json forbidden.json   # puis complète-le : sans lui, le serveur refuse de démarrer
./sloubi                                     # http://127.0.0.1:8888
```

Pour la prod : derrière un proxy (Caddy), avec les drop-ins systemd de `deploy/` (`hardening.conf`, et `turn.conf` pour le relais des appels).

Configuration : copie `.env.example` en `.env` (lu au démarrage, ignoré par git) pour `ADMIN_EMAIL` et l’envoi des codes de connexion (SMTP, par exemple Gmail avec un mot de passe d’application, ou l’API Brevo). Sans envoi configuré, les codes s’écrivent dans le journal du service.

### Depuis les sources

```
go run .                 # http://127.0.0.1:8888
PORT=8080 go run .       # autre port
HOST=0.0.0.0 go run .    # écouter sur toutes les interfaces (par défaut : local seulement, Caddy est devant)
```

Tests :

```
go test -race ./...
```

Déploiement (prod) : `bin/build.sh` builde, vérifie, puis redémarre le service systemd `sloubi`.

Builder le binaire :

```
go build -o sloubi .
./sloubi
```

Le serveur cherche `assets/`, `views/`, `db/` et `forbidden.json` **dans le répertoire courant** : lance-le depuis la racine du projet (ou copie ces dossiers à côté du binaire).

Audit des dépendances :

```
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## API

| Méthode | Route | Rôle |
|---|---|---|
| `GET` | `/` | Accueil. |
| `GET` | `/_/domaine?domain=…` | Pour le TLS à la demande du proxy, en local seulement : 200 si le domaine est rattaché à un lieu, 404 sinon. |
| `GET` | `/_/compte` | Mon compte : connexion par code e-mail, mes lieux, et pour l’admin les demandes à valider. |
| `GET` | `/_/` | Le registre : tous les salons, les plus récemment actifs d’abord (sloubis, messages, dernière activité). `noindex`, cache de 15 s. Aucun salon ne peut commencer par `_`, donc pas de collision. |
| `GET` | `/<chan>` | Page du salon. Majuscules → redirection 301 ; nom invalide ou interdit → 404 (accueil, nom en rouge). |
| `GET` | `/api/check/<chan>` | `{name, ok, reason, exists, theme, locked}` ; `reason` ∈ `invalid`, `toolong`, `forbidden`. |
| `POST` | `/api/<chan>/messages` | `{clientId, nickname, message}` en `application/json` → `202`. Le message revient par le WebSocket. Sans pseudo valide : `400 {"error": "nickname required"}`. |
| `GET` | `/api/<chan>/last-sloubi` | `{number, exists, nickname, message, timestamp}` du dernier sloubi validé. |
| `POST` | `/api/nick` | `{nickname}` → `{ok, nickname, reason}` ; `reason` ∈ `tooshort`, `toolong`, `chars`, `forbidden`. |
| `POST` | `/api/<chan>/postit` | `{clientId, text}` → le post-it, diffusé à tout le salon. |
| `GET` | `/ws/<chan>` | WebSocket : 100 derniers messages et post-it, puis le direct. Événements `{type: "message" \| "postit" \| "room", ...}` ; `room` (nouveau décor ou nouvelle serrure) précède une déconnexion, le client recharge. Le client envoie `{type: "call", op: "join" \| "leave" \| "signal" \| "state" \| "live" \| "handoff" \| "handoff-decline"}` (et `{type: "hello", push}` pour dire quel abonnement aux notifications est le sien) ; le serveur répond `joined`, `full`, `denied`, `nick-required`, `stage-busy`, `handoff`, `handoff-declined`, `signal` (relayé au seul destinataire) et `roster` (à tout le salon, avec `live` : qui est sur scène). |
| `GET` | `/api/<chan>/info` | `{exists, theme, locked, access, owner, claimable}`, plus `{messages, lastId, sloubi}` si on a accès. En-tête `X-Owner-Key` pour savoir si l’on est le créateur. |
| `POST` | `/api/<chan>/settings` | `{ownerKey, theme?, password?, removePassword?}`. Sur un lieu qui n’existe pas, le fonde et renvoie `ownerKey`. Thèmes : `taverne`, `foret`, `moulin`, `cyber`, `kawaii`, `bd`. Mot de passe de 4 à 128 caractères, **avec une session** (`401 login required` sinon) ; pour un membre pas encore validé, la réponse porte `pending: true` et le lieu reste ouvert. Un lieu rattaché à un compte ne se règle plus avec la clé seule. |
| `POST` | `/api/_compte/code` | `{email}` → envoie un code à 6 chiffres (10 min ; un renvoi par minute). |
| `POST` | `/api/_compte/verify` | `{email, code}` → crée le compte au besoin et pose la session (`sloubi_session`, HttpOnly, 90 jours). |
| `GET` | `/api/_compte/me` | `{signedIn, email, role, status, rooms}` ; `status` ∈ `aucun`, `en-attente`, `valide`, `refuse`. |
| `POST` | `/api/_compte/logout` | Ferme la session. |
| `GET` | `/api/_admin/users` | Admin : les comptes et leurs lieux, demandes en attente d’abord. |
| `POST` | `/api/_admin/users/<id>` | Admin : `{action: "valider" \| "refuser" \| "retirer"}`. Valider active les mots de passe demandés. |
| `POST` | `/api/<chan>/unlock` | `{password}` → pose le cookie d’accès (`HttpOnly`, 30 jours). |
| `GET` | `/api/<chan>/turn` | Serveurs ICE d’un appel : STUN, et TURN avec identifiants éphémères (6 h) si `TURN_SECRET` ou `TURN_SECRET_FILE` est défini. Lieu gardé et cookie d’accès requis. |
| `GET` | `/api/push-key` | Clé publique VAPID des notifications. |
| `POST` | `/api/<chan>/push` | `{endpoint, keys: {p256dh, auth}, tag}` : s’abonner aux appels d’un lieu gardé ; `{endpoint, remove: true}` pour se désabonner. Seuls les services de push connus sont acceptés. |
| `GET` | `/api/<chan>/microbiotes[/<mot>]` | Les `#mots` du lieu et leur nombre, ou l’encyclopédie d’un mot (100 derniers messages). |

Les anciennes routes `/verif.json`, `/last-sloubi.json` et `/ws` restent des alias de `/chante` pour les PWA en cache.

Un salon est créé à son premier message (ou post-it, ou réglage), pas à la simple visite. La réponse qui le fonde porte `ownerKey`, une seule fois.

Sur un lieu verrouillé, toutes les routes de l’API du salon et le WebSocket répondent `403` sans le cookie d’accès.

---

## Stockage

- **SQLite** (`db/chat.sqlite`, mode WAL) : tables `messages` (avec `channel`), `channels` (avec `theme`, l’empreinte SHA-256 de la clé du créateur, le compte propriétaire, le mot de passe haché et celui qui attend l’admin), `users` et `otp_codes`, `postits`, `tags` (les `#mots`) et `meta` (la clé HMAC des cookies d’accès). Les migrations tournent au démarrage ; les messages d’avant les salons sont dans `chante`.  
- Le serveur calcule les sloubis (le client ne décide rien), de façon atomique : deux `sloubi N` simultanés n’en valident qu’un (`UNIQUE(channel, sloubi_number)`).  
- Horodatage côté serveur.  

---

## Pourquoi “sloubi” ?

Parce que c’est le mot magique.  
Parce que dans un monde saturé de plateformes centralisées, il fallait un signal simple, drôle, et partagé.  
Parce que c’est un pied de nez :  
- Aux fermetures arbitraires de services.  
- Aux discussions modérées par des algorithmes.  
- À la dépendance aux GAFAM pour juste parler entre humains.

Ici, **sloubi = liberté**.

---

## Sécurité & Vie privée

- Pas de tracking tiers, pas de pub, pas d’export des données utilisateurs.  
- Pas de compte pour discuter : ton pseudo et ton `clientId` restent dans ton navigateur ; le `clientId` n’est jamais renvoyé aux autres. Le compte (une adresse e-mail, connexion par code à 6 chiffres valable 10 minutes, 5 essais, codes gardés hachés) ne sert qu’à garder un lieu par mot de passe.  
- Mots de passe hachés en PBKDF2-SHA256 (210 000 itérations, sel aléatoire), essais limités par IP avec ban temporaire. Le cookie d’accès est un HMAC lié au hachage : changer le mot de passe invalide tous les cookies. La clé du créateur n’est stockée que sous forme d’empreinte. Un lieu verrouillé apparaît dans `/_/` avec un cadenas, sans ses chiffres.  
- Messages et post-its affichés en texte brut (`textContent`), caractères de contrôle retirés côté serveur.  
- Limites : message 500 caractères, pseudo 24, post-it 280, corps de requête 4 Ko. Un pseudo invalide envoyé avec un message devient `Anonyme`.  
- Rate-limit par IP (IPv6 : par préfixe /64) sur tout ce qui écrit, sur les WebSockets et sur `/api/check` (ban temporaire pour les bots qui énumèrent), plus un plafond large sur toutes les requêtes. 12 WebSockets simultanés par IP au plus.  
- Timeouts HTTP (en-têtes en 5 s : pas de slowloris), en-têtes de 16 Ko maximum, Gin en mode release.  
- Textes libres normalisés : NFC, sans caractères de direction ni invisibles (le ZWJ des emojis est gardé), diacritiques plafonnés à 2 par lettre (pas de zalgo).  
- `forbidden.json` : routes, admin, marques, sondes de bots, injures ; comparaison après normalisation (leet, séparateurs). Jamais servi au client.  
- WebSocket limité à la même origine, POST en `application/json` uniquement.  
- En-têtes : CSP `default-src 'none'` sans style ni script inline, HSTS, `X-Frame-Options`, `nosniff`, COOP/CORP `same-origin`, Permissions-Policy fermée.  
- Service systemd durci : `deploy/hardening.conf` (utilisateur `www-data`, système en lecture seule sauf `db/`, aucune capacité, appels système filtrés ; score `systemd-analyze security` de 1,2).  
- Base : WAL reporté dans `chat.sqlite` au démarrage puis toutes les heures.  
- Polices et Three.js auto-hébergés (`assets/fonts/` OFL, `assets/vendor/three/` MIT) : aucune requête vers un tiers.  
- Dépendances à jour, `govulncheck` sans vulnérabilité atteignable.  

---

## Roadmap

- [x] REST + WS avec Gin  
- [x] SQLite pour la persistance  
- [x] Compteur sloubi validé côté serveur  
- [x] PWA installable  
- [x] Durcissement (limites, rate-limit, origine WS, horodatage serveur)  
- [x] Salons : `sloubi.chat/<chan>`, accueil, post-it  
- [ ] Purge des salons inactifs, PWA par salon (voir [TODO.md](TODO.md))  
- [ ] Markdown safe (goldmark + bluemonday)  
- [ ] GIF selector (Kaamelott par défaut)  
- [ ] Emoji picker (recherche + récents)  
- [ ] Auth via OAuth  
- [ ] Hébergement fédéré (serveurs Sloubi interconnectés)  

---

## Licence

MIT.  

Prends, copie, hacke.  

Fais tourner ton propre sloubi-server et arrêtons d’attendre qu’un géant décide si on a encore le droit de parler.
