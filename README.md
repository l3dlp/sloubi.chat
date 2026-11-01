# Sloubi.chat — le chat médiéval indépendant

Meta a fermé les discussions de groupe ?  
On ne va pas pleurer, on va forger notre propre taverne.

**Sloubi** est une réponse directe à la fermeture des groupes de discussion par Meta.  
Plutôt que de se laisser enfermer dans des walled gardens qui tirent la prise quand ça les arrange, on s’est construit un **chat indépendant, libre et fun**, centré autour d’un seul mot magique : **sloubi**.

Ici, pas d’algos, pas de pub, pas de flicage. Juste un compteur communautaire qui monte à chaque sloubi validé.

---

## Fonctionnalités

- **Ta taverne en une URL** : sur l’accueil, tape un nom, tu as ton `sloubi.chat/<nom>`.  
  Le nom vire au vert émeraude s’il est libre (ou existe déjà, tu le rejoins), au rouge 8 bits s’il est invalide ou réservé.  
  Règle : `a-z`, `0-9`, et `.` `_` `-` jamais doublés ni en bord, 32 caractères max.  
- **La taverne historique** vit sur [`/chante`](https://sloubi.chat/chante), en hommage à *Perceval chante Sloubi* (Kaamelott, livre III) ; un lien direct depuis l’accueil.  
- **Chaque salon est une taverne enfumée.** Une scène 3D (Three.js) où dérivent des volutes éclairées par l’âtre, des rais de lumière, des braises, des chopes de bière et des dés qui flottent. Les messages se condensent hors de la fumée et s’y dissolvent en montant ; chaque sloubi validé fait jaillir une gerbe d’étincelles du compteur. La scène s’allège toute seule sur une machine lente, se fige avec `prefers-reduced-motion`, et laisse place à un fond peint sans WebGL.  
- **`/nick <pseudo>`** pour changer de pseudo (aussi dans les paramètres). Le serveur le valide et renvoie sa forme canonique : alphabet latin, chiffres, espace et `_ - . '`, 2 à 24 caractères, Unicode normalisé (NFC), noms réservés refusés. Pas de sosies cyrilliques, de caractères invisibles ni de zalgo.  
- **Chat en temps réel** par salon : REST pour poster, WebSocket pour recevoir.  
- **Sloubi compteur** par salon, arbitré par le serveur :  
  - il faut écrire `sloubi <N>` (casse indifférente, ex. `Sloubi 42`) où **N est exactement le prochain numéro** ;  
  - on ne peut pas valider deux sloubis d’affilée (le même `clientId` que le dernier sloubi validé est refusé) ;  
  - un message qui ne remplit pas ces conditions est publié normalement, sans compter.  
- **Post-it partagé** sur chaque salon : 280 caractères, modifiable par tous, mis à jour en direct. Déplaçable, refermable en punaise ; position propre à chaque visiteur.  
- **Pseudo libre**, gardé dans le `localStorage` du navigateur (`Anonyme` par défaut).  
- **Mode Triche** (réglages) : affiche un bouton raccourci ; la validation reste côté serveur.  
- **Ambiance de taverne** : messages d’ambiance aléatoires côté client.  
- **PWA installable** : manifest et icônes.  

---

## Structure

```
sloubi.chat/
├─ main.go              # point d’entrée (lit PORT, défaut 8888)
├─ go.mod / go.sum      # Go 1.27.3, Gin, gophre/websocket, modernc SQLite (pur Go)
├─ forbidden.json       # noms de salon interdits (rechargé à chaud)
├─ bin/build.sh         # build + govulncheck + redémarrage du service
├─ src/
│  ├─ chat/message.go   # Message et Postit (JSON partagé REST / DB / WS)
│  └─ web/
│     ├─ routes.go      # routes HTTP + WS, nettoyage des saisies
│     ├─ db.go          # SQLite, migrations, règle de validation du sloubi
│     ├─ channels.go    # règle des noms de salon + forbidden.json
│     ├─ limiter.go     # rate-limit par IP
│     ├─ message.go     # Hub WebSocket par salon
│     └─ websocket.go   # upgrader WS (même origine)
├─ views/
│  ├─ home.html         # accueil : le gros /________
│  └─ room.html         # page d’un salon : la taverne enfumée
├─ assets/              # app.js (chat), room-fx.js (scène 3D), home.js, CSS, fonts/, vendor/three/, manifest, icônes
└─ db/chat.sqlite       # base SQLite (auto-créée, ignorée par git)
```

---

## Lancer

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
| `GET` | `/<chan>` | Page du salon. Majuscules → redirection 301 ; nom invalide ou interdit → 404 (accueil, nom en rouge). |
| `GET` | `/api/check/<chan>` | `{name, ok, reason, exists}` ; `reason` ∈ `invalid`, `toolong`, `forbidden`. |
| `POST` | `/api/<chan>/messages` | `{clientId, nickname, message}` en `application/json` → `202`. Le message revient par le WebSocket. |
| `GET` | `/api/<chan>/last-sloubi` | `{number, exists, nickname, message, timestamp}` du dernier sloubi validé. |
| `POST` | `/api/nick` | `{nickname}` → `{ok, nickname, reason}` ; `reason` ∈ `tooshort`, `toolong`, `chars`, `forbidden`. |
| `POST` | `/api/<chan>/postit` | `{clientId, text}` → le post-it, diffusé à tout le salon. |
| `GET` | `/ws/<chan>` | WebSocket : 100 derniers messages et post-it, puis le direct. Événements `{type: "message" \| "postit", ...}`. |

Les anciennes routes `/verif.json`, `/last-sloubi.json` et `/ws` restent des alias de `/chante` pour les PWA en cache.

Un salon est créé à son premier message (ou post-it), pas à la simple visite.

---

## Stockage

- **SQLite** (`db/chat.sqlite`, mode WAL) : tables `messages` (avec `channel`), `channels` et `postits`. Les migrations tournent au démarrage ; les messages d’avant les salons sont dans `chante`.  
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
- Pas de comptes : ton pseudo et ton `clientId` restent dans ton navigateur ; le `clientId` n’est jamais renvoyé aux autres.  
- Messages et post-its affichés en texte brut (`textContent`), caractères de contrôle retirés côté serveur.  
- Limites : message 500 caractères, pseudo 24, post-it 280, corps de requête 4 Ko. Un pseudo invalide envoyé avec un message devient `Anonyme`.  
- Rate-limit par IP sur tout ce qui écrit, sur les WebSockets et sur `/api/check` (ban temporaire pour les bots qui énumèrent).  
- `forbidden.json` : routes, admin, marques, sondes de bots, injures ; comparaison après normalisation (leet, séparateurs). Jamais servi au client.  
- WebSocket limité à la même origine, POST en `application/json` uniquement, CSP stricte.  
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
