# TODO — vers `sloubi.chat/<chan>`

Objectif : sur l’accueil, on tape le nom de son salon et on atterrit sur `sloubi.chat/<chan>`, une taverne à soi avec son propre compteur et son post-it.

La taverne actuelle (servie sur `/`) devient le salon **`/chante`**, en référence à *Perceval chante Sloubi* (Kaamelott, livre III, épisode 50). `/` devient la page d’accueil.

**État au 31/10/2026 :** le multi-salon, l’accueil, le post-it et le durcissement sont faits (cases cochées). Restent les cases vides.

---

## 0. Règle du nom de salon

Caractères autorisés : `a-z`, `0-9`, `.`, `_`, `-`. Un seul segment d’URL (pas de `/`).

Les séparateurs (`.`, `_`, `-`) ne sont jamais doublés, ni au début ni à la fin :

```
^[a-z0-9]+([._-][a-z0-9]+)*$
```

| Accepté | Refusé |
|---|---|
| `kaamelott` | `a--b`, `a._b` (séparateurs doublés) |
| `mon-chan` | `.x`, `-x`, `x_` (séparateur au début ou à la fin) |
| `mon-chan.v2` | `mon chan`, `école`, `a/b` (caractères interdits) |
| `table_ronde-2026` | tout nom de plus de 32 caractères |

- [x] Longueur 1 à 32 caractères.
- [x] Forme canonique en minuscules : `/MonChan` redirige (301) vers `/monchan`.
- [x] Même regex côté serveur (qui fait foi) et côté client (retour immédiat dans le champ de saisie).
- [x] Le `.` impose de bien trier les routes : un fichier statique ne doit pas être pris pour un salon, et inversement. Une fois l’API sous `/api/` et les fichiers sous `/assets/`, seule la liste des noms interdits reste à maintenir.

### Noms interdits : `forbidden.json`

Le fichier est à la racine du repo. Il contient les routes et fichiers existants, les noms d’admin et d’autorité, les marques, les sondes de bots (`wp-admin`, `phpmyadmin`…) et les injures. Trois listes :

- `exact` : le nom normalisé est égal à l’entrée. Pour les mots courts ou courants (`meta`, `apple`, `pute`…), qui donneraient des faux positifs en sous-chaîne (`metal`, `computer`, `dispute`).
- `contains` : le nom normalisé contient l’entrée (`admin`, `paypal`, `support`…). Entrées de 5 lettres ou plus.
- `allow` : sous-chaînes retirées avant le test `contains`, pour les faux positifs connus (`badminton`).

La normalisation passe en minuscules, retire `.`, `_` et `-`, et convertit le leet (`0→o 1→i 3→e 4→a 5→s 7→t 8→b 9→g @→a $→s`). Ainsi `4dm1n`, `super.admin`, `w-s` et `sl0ub1` sont bloqués. Prototype testé sur une vingtaine de noms : aucun faux positif sur `amelie`, `calcul`, `violet`, `metal`, `free-party` ou `badminton-club`.

- [x] Charger `forbidden.json` au démarrage. Si le fichier est absent ou invalide, refuser de démarrer plutôt que tout autoriser.
- [x] Recharger à chaud quand le fichier change (mtime vérifié toutes les 30 s, ou `SIGHUP`), pour bannir un nom sans redéployer.
- [x] Ne pas servir le fichier au client : la vérification passe par l’API (section 1), sinon les bots ont la liste toute prête.
- [x] Tests unitaires de la normalisation et des trois listes, avec les exemples ci-dessus.

## 1. Routage

- [x] Déplacer l’API sous un préfixe pour libérer l’espace de noms : `/api/:chan/messages` (POST), `/api/:chan/last-sloubi`, `/ws/:chan`. Garder les anciennes routes comme alias du salon par défaut pendant la transition.
- [x] `GET /:chan` : valide le nom, sinon 404 propre. Remplacer le `NoRoute` qui sert `index.html` partout.
- [x] `GET /` : page d’accueil (section 4).
- [x] `GET /api/check/:chan` : renvoie `{"ok": bool, "reason": "invalid|forbidden|", "exists": bool}`. Même code de validation que `GET /:chan`. Rate-limité (section 5).
- [x] Transition `/chante` : les anciennes routes (`/verif.json`, `/ws`, `/last-sloubi.json`) deviennent des alias du salon `chante` le temps que les clients en cache (PWA installées) se mettent à jour, puis on les supprime.
- [x] `views/index.html` charge `styles.css` et `app.js` en **chemins relatifs** : ça casse sur `/chan/` (slash final). Passer en chemins absolus (`/assets/...`) et rediriger `/chan/` vers `/chan`.

## 2. Base de données

- [x] Ajouter une colonne `channel TEXT NOT NULL DEFAULT 'chante'` avec une migration (`ALTER TABLE` si la colonne est absente). Les messages existants (et leur compteur) vont dans `chante`.
- [x] Index `(channel, id)` pour le backlog et `(channel, sloubi_number)` pour le compteur.
- [x] `lastSloubiEntry`, `lastMessages` et `lastSloubi` filtrés par salon.
- [x] **Course critique sur le compteur** : la vérification « N = max + 1 » et l’`INSERT` ne sont pas atomiques, deux `sloubi N` simultanés passent tous les deux. Ajouter une contrainte `UNIQUE(channel, sloubi_number)` et faire la vérification et l’insertion dans une transaction (ou une seule `INSERT ... SELECT`). C’est déjà un bug avec un seul salon.
- [x] Table `channels` (nom, créé le, dernier message) : sert à `exists` dans `/api/check`, aux stats et à la purge des salons inactifs. Un salon est créé au premier message, pas à la simple visite, pour que les bots qui testent des URL ne remplissent pas la table.
- [x] Table ou colonne pour le post-it : `channel`, `text`, `updated_at`, `updated_by` (section 4).

## 3. Hub WebSocket

- [x] Remplacer l’ensemble global `clients` par `map[channel]map[*Conn]`, et diffuser uniquement aux connexions du salon.
- [x] Supprimer l’entrée d’un salon quand il n’a plus de connexions.
- [x] **Écritures concurrentes sur une même connexion** : le handler `/ws` envoie le backlog pendant que `Hub.Run` peut écrire sur la même connexion. Les connexions WebSocket (API style gorilla) ne supportent pas les écritures concurrentes, ce qui donne une data race ou un panic. Il faut un goroutine d’écriture par client avec un channel `send` bufferisé.
- [x] Un client lent bloque toute la diffusion (`WriteJSON` sans délai, sous `RLock`). Ajouter `SetWriteDeadline`, ping/pong, et déconnecter le client si son buffer `send` est plein.

## 4. Frontend

### Accueil `/`

Un énorme `/________` au centre de l’écran, rien d’autre qui distraie.

- [x] Focus déjà dans le champ au chargement, curseur bloc qui clignote en grand.
- [x] Saisie forcée en minuscules ; les caractères hors `a-z0-9._-` sont ignorés à la frappe.
- [x] Validation en direct :
  - regex et longueur côté client, immédiatement ;
  - puis `GET /api/check/:chan` avec un debounce de 300 ms, en annulant la requête précédente (`AbortController`).
- [x] **Nom disponible → vert émeraude, fantasy** : lueur douce, police médiévale déjà chargée (`MedievalSharp` / `Uncial Antiqua`), éventuellement quelques étincelles dorées. Sous-texte « Entrée → fonder ta taverne » si le salon est neuf, « Entrée → rejoindre la taverne » s’il existe déjà.
- [x] **Nom invalide ou interdit → rouge 8 bits, dernier boss du monde de la lave** : police pixel, tremblement court, scanlines, rouge et orange lave qui pulse. Sous-texte qui dit pourquoi (« caractère interdit », « nom réservé »), sans afficher quelle entrée de `forbidden.json` a matché.
- [x] Entrée sur un nom vert → `location.href = '/' + nom`. Sur un nom rouge, rien (le tremblement rejoue).
- [x] Une police pixel en plus : la charger depuis Google Fonts (`Press Start 2P` ou `Silkscreen`), comme les polices actuelles.
- [x] `prefers-reduced-motion` : pas de tremblement ni d’étincelles, juste la couleur.
- [x] Optionnel : lien discret vers `/chante` et salons récents du visiteur (`localStorage`).

### Salon `/<chan>`

- [x] Déduire le salon de `location.pathname` et l’utiliser dans le POST, le WS et `last-sloubi`.
- [x] Afficher le nom du salon dans l’en-tête et le `<title>`, plus un bouton « copier le lien ».

### Post-it (sur chaque salon)

- [x] Un post-it par salon, au **contenu partagé** : stocké en base, diffusé par WS à tous les présents. Texte brut, 280 caractères maximum, le dernier qui écrit gagne.
- [x] Éditable par tout le monde dans le salon, comme le chat. Rate-limité, et filtré comme un message.
- [x] **Déplaçable** (Pointer Events, donc souris et tactile), contraint dans la fenêtre, recalé dans l’écran au redimensionnement.
- [x] **Refermable** : réduit à une petite punaise qu’on rouvre d’un clic.
- [x] Position et état ouvert/fermé **propres à chaque visiteur**, par salon (`localStorage`, clé `sloubi_postit_<chan>`), lus et écrits dans un `try/catch`.
- [x] Si le contenu change pendant que le post-it est fermé, la punaise l’indique (point ou rebond).
- [x] Accessible au clavier : déplacement aux flèches quand il a le focus, `Échap` pour le refermer.

### Divers
- [x] Le bouton « sloubi rapide » du Mode Triche envoie `sloubi` sans numéro, ce qui ne valide jamais rien. Il doit envoyer `sloubi ${sloubiCount + 1}`.
- [x] `renderMessage` injecte `msg.html` via `innerHTML` alors que le serveur n’envoie jamais ce champ. Le retirer, ou ne le brancher qu’après la sanitization serveur (goldmark + bluemonday).
- [ ] PWA : le manifest a `start_url` à `/`. Décider si on installe la PWA par salon (manifest dynamique) ou si on garde l’accueil.

## 5. Sécurité / anti-abus

Créer des salons à volonté ouvre la porte au spam. Le minimum avant d’ouvrir :

- [x] Limite de taille du corps HTTP (`http.MaxBytesReader`), du message (500 caractères par exemple) et du pseudo (32 caractères, caractères de contrôle filtrés). Il n’y en a aucune aujourd’hui.
- [x] Rate-limit par IP : messages 1/s (rafale 5), post-it 1 toutes les 3 s, WS 1 toutes les 2 s (rafale 10), 10 nouveaux salons par heure, `/api/check` 4/s (rafale 10) avec ban de 10 min après 30 refus. `TrustedProxies` limité à Caddy (127.0.0.1).
- [x] `CheckOrigin` du WebSocket qui renvoie `true` : restreindre à l’origine du site.
- [x] Horodatage : le serveur réutilise le `timestamp` envoyé par le client. Toujours utiliser l’heure serveur.
- [x] `clientId` n’est plus diffusé aux autres clients : on ne peut plus se faire passer pour quelqu’un d’autre.
- [ ] On peut toujours contourner « pas deux sloubis d’affilée » en vidant son `localStorage` (nouvel ID). Un cookie signé ne change rien à ça ; une règle par IP gênerait les amis sur le même wifi. À trancher si ça devient un problème.
- [x] Rejeter les messages vides (le serveur stocke actuellement un message vide si un pseudo est présent).
- [x] Taille de `ReadBuffer` / `SetReadLimit` sur le WS (le client n’est pas censé envoyer quoi que ce soit).
- [ ] Purge ou archivage des salons inactifs.

- [x] En-têtes de sécurité (CSP, `X-Frame-Options`, `nosniff`) ; le serveur n’écoute plus que sur 127.0.0.1 par défaut (`HOST` pour changer).

- [x] Audit du 01/11/2026 : timeouts HTTP (slowloris), clés de rate-limit IPv6 en /64, plafond de WS simultanés, plafond global de requêtes, texte libre sans caractères de direction ni zalgo, CSP sans style inline, HSTS/COOP/CORP/Permissions-Policy, Gin en mode release, checkpoint du WAL.
- [ ] Installer `deploy/hardening.conf` sur le service systemd (aujourd'hui il tourne en root, score 9,6 UNSAFE ; validé à 1,2 sur une instance de test).

## 6. Repo / exploitation

- [ ] Le binaire `sloubi` (≈ 35 Mo) est versionné. Le sortir de git (`.gitignore`) et le publier en release.
- [ ] Ajouter le Dockerfile promis dans l’ancienne doc, ou acter qu’il n’y en aura pas.
- [x] Tests : au minimum la règle de validation du sloubi (`insertMessage`), la regex des noms de salon, `forbidden.json` et l’isolation entre salons.
