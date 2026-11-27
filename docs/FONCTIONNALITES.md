# Fonctionnalités

Ce que voit et fait l’utilisateur, au 22/11/2026 (v1.0.0, plus le partage d’écran et les comptes).

Le cap fixé par l’auteur : **une messagerie privée améliorée**, « graphique et comprise par la mamie de 72 ans ». Le décor sert l’ambiance ; on doit toujours voir tout de suite où est le fil de discussion.

## L’accueil (`/`)

- Sobre, dans la langue visuelle de NADAPROD (`/intelligences/nadaprod/web/`) : papier ou noir chaud selon le thème du système, une seule couleur d’accent, titres en Permanent Marker, texte en Archivo, traits dessinés au marqueur (`marker.css`).
- Un grand champ `/________` : on tape un nom de salon. Le champ dit tout de suite si le nom est libre, s’il existe déjà (et dans quel décor) ou s’il est refusé, et pourquoi.
- **Mot de passe dès l’accueil** : pour un salon gardé, Entrée fait apparaître un champ de mot de passe qui appelle `/unlock`, et l’on arrive déverrouillé.
- La page présente les six lieux, les usages (messagerie, fichiers, vocaux, appels), l’histoire du nom et la licence MIT.
- **L’histoire du nom** : un nom anecdotique, né d’un râle contre la suppression des discussions à plusieurs sur Facebook, qui a emporté une chouette communauté. Le nom vient de *Perceval chante Sloubi* (Kaamelott, livre III).
- Le dé 3D de navigation a été essayé puis **retiré à la demande** (« oublie toute cette histoire de dé »). Ne pas le remettre.

## Le salon (`/<chan>`)

### Mise en page
- **Barre du haut** : Sloubi.chat (retour à l’accueil), le nom du salon, un **petit score** (le compteur du thème, devenu anecdotique), ⚙ Options.
- **Barre d’icônes** toujours visible : en haut en portrait, rail à gauche en paysage (700 px et plus), cachée quand le clavier du téléphone est ouvert. Entrées : Discussion, Favoris, Post-it, et dans un salon gardé Images, Vidéos, Documents, avec le nombre de fichiers en pastille. Elle remplace un menu hamburger jugé « trop chiant ».
- **Le fil** dans un panneau bordé, avec des bulles à la Messenger : les miennes à droite, celles des autres à gauche avec un avatar (initiale), l’heure, des séparateurs de jour.

### Messages
- Liens cliquables, ouverts dans un nouvel onglet.
- **Plus aucun message d’ambiance** (25/11/2026) : le fil ne dit que qui arrive (« → Arthur arrive. ») et qui s’en va (« ← Arthur s’en va. »), plus « Déjà là : … » à l’arrivée. Un rechargement ou une reconnexion ne disent rien.
- **Pièces jointes** : mes fichiers, vocaux et vidéos sont à droite comme mes messages ; images et vidéos sont des vignettes de 48 px de haut ; un clic ouvre la visionneuse, et les flèches ← → (ou le doigt) parcourent toutes les vignettes du fil, ou de la vue ouverte.
- **Cœur sur n’importe quel message** (double-clic, ou le bouton). On ne compte **que des totaux** : cœurs par message, clics par lien. Personne n’est pisté.
- `/nick <pseudo>` change le pseudo, comme les réglages.
- **Pseudo obligatoire** avant d’écrire ou d’appeler : tant qu’il manque, le champ de saisie le demande. « Anonyme » est réservé.
- **Post-it partagé** : 280 caractères, modifiable par tous, déplaçable, refermable ; jamais posé par-dessus la barre d’icônes.
- **Mode Triche** (réglages) : un bouton « sloubi rapide » ; la validation reste côté serveur.

### Les six lieux

| Clé | Lieu | Scène | Petit jeu (score) |
|---|---|---|---|
| `taverne` | La taverne (défaut) | 3D : fumée, âtre, braises, chopes, dés | compter les sloubis (`sloubi <N>`, pas deux d’affilée) |
| `foret` | La forêt nourricière en permaculture | 3D : sous-bois, lucioles, microbiotes | chaque message est une graine ; tous les 10, un arbre. Un `#mot` fait pousser un microbiote, qui ouvre son encyclopédie |
| `moulin` | Le moulin au bord de la rivière | 3D : eau en reflet miroir, roue | chaque message lance une barque de papier et donne un tour de roue ; tous les 10, un sac de farine |
| `cyber` | Cyberpunk 1984, cousin du punk | 2D : fanzine photocopié, collages, grille fluo, VHS | mots collés au mur, texte qui se déchiffre, pseudos en lettres anonymes, narrateur `SYS>` |
| `kawaii` | Kawaii | 2D : ciel pastel, nuages et mochis avec des yeux | le bouton d’envoi est un personnage (suit le curseur, s’endort, sourit) ; arc-en-ciel tous les 10 |
| `bd` | BD franco-belge | 2D : planche en ligne claire | bulles gauche/droite, cartouches, onomatopées ; page tournée toutes les 6 cases |

- Les trois derniers sont « dans l’esprit *There Is No Game 2* ». Kawaii et BD sont des thèmes clairs.
- **La forêt et le moulin suivent l’heure du visiteur** : aube, jour, couchant, nuit (étoiles, fenêtres du meunier, lucioles). `?heure=14` force une heure.
- Chaque thème a ses bulles, ses couleurs et **ses barres de défilement**.
- **Sur téléphone et tablette, pas de scène** (25/11/2026) : elle saccadait et se voyait à peine sous le fil. À la place, un fond très léger aux couleurs du thème (shader dessiné en petit, 20 images/s), pensé pour un iPhone 11. Les habillages des thèmes 2D (lettres anonymes, bouton à yeux, onomatopées) n’y sont plus non plus. `?3d` remet la scène, pour essayer.
- Les scènes s’allègent sur une machine lente, se figent avec `prefers-reduced-motion`, et laissent un fond peint sans WebGL.

### Le créateur et les réglages
- Le **créateur** est celui qui fonde le salon (premier message, post-it ou réglage). Il reçoit une clé gardée dans son navigateur, et un **lien de créateur** (`/<chan>#cle=…`) pour un autre appareil.
- Réglages, chacun appliqué tout de suite :
  - cliquer une carte de thème l’applique à l’instant pour tout le monde ;
  - pseudo et mot de passe ont chacun leur bouton **Changer → En cours… → Changé ✓** ;
  - retirer le mot de passe rouvre le salon.
- Changer le thème ou le mot de passe recharge tout le monde ; changer le mot de passe met dehors ceux qui ne l’ont pas.
- Les salons qui existaient avant les comptes, `/chante` compris, appartiennent à l’admin. Un salon fondé par quelqu’un de connecté est rattaché à son compte.

## Les salons gardés (avec mot de passe)

Le mot de passe ouvre tout le reste : fichiers, vocaux, appels, notifications.

### Médiathèque
- **Envoi à l’arrache** : trombone, glisser-déposer n’importe où, coller. Trois envois en parallèle avec leur progression.
- **Classement automatique** d’après le contenu (pas le nom) : Images, Vidéos, Documents (les sons sont rangés en audio et s’écoutent dans le fil).
- Miniatures des photos (orientation EXIF respectée, PNG transparents sur fond blanc), visionneuse, téléchargement.
- **1001 Go par salon** (choix du 22/11/2026, les fichiers partiront sur un CDN bunny.net avant l’ouverture), jauge d’espace en haut des vues médias. Le serveur s’arrête de toute façon à 5 Go libres sur son disque.
- **Vidéos** : les fichiers vidéo, plus les liens YouTube, Vimeo et Dailymotion partagés dans le fil, avec miniature et titre (récupérés par le serveur), ouverts dans un nouvel onglet.
- **Documents** : les fichiers non médias, plus **tous les autres liens** partagés, avec leur total de clics.
- **Favoris** : les messages les plus aimés du salon, ou « mes cœurs ».

### L’enregistreur maison (le « + »)
Le « + » à côté du trombone ouvre un menu :

| Choix | Capture | Durée max | Compression |
|---|---|---|---|
| Message vocal | micro | 15 min | Opus 32 kbit/s (≈ 240 Ko la minute) |
| Vidéo | caméra + micro | 5 min | 720p à 1,2 Mbit/s (≈ 9 Mo la minute) |
| **Mon écran + micro** | `getDisplayMedia` + micro | 10 min | jusqu’à 1080p, 15 i/s, 1,5 Mbit/s, son 64 kbit/s |

- On réécoute ou on revoit avant d’envoyer ; le navigateur compresse pendant l’enregistrement (`MediaRecorder`, MP4 sur Safari, WebM ailleurs).
- **Partage d’écran** (ajouté le 21/11/2026) : « pour montrer quelque chose sans la cam ». On choisit un onglet, une fenêtre ou tout l’écran ; pendant l’enregistrement, la fenêtre se réduit à une pastille en bas (chrono + Arrêter) pour ne pas cacher ce qu’on montre. Arrêter le partage depuis le navigateur arrête l’enregistrement. Sans micro, l’écran est enregistré sans le son. Le choix est caché sur les écrans tactiles et quand `getDisplayMedia` manque.
- Fichiers nommés « Message vocal… », « Vidéo… », « Écran JJ-MM HHhMM.webm ».

### Sa caméra (live et enregistreur vidéo, 25/11/2026)
- **Changer de caméra** quand on en a plusieurs (de face / de dos sur un téléphone) : un bouton rond sur sa propre image, en live comme dans l’enregistreur, même en pleine prise.
- **Régler son image** : clarté, luminosité, noir et blanc, teinte, avec « Réinitialiser ». Les autres voient l’image réglée, et la vidéo enregistrée la garde. Les réglages sont retenus par le navigateur.
- Sans réglage, le flux de la caméra part tel quel (aucun calcul en plus).

### Appels de groupe
- Jusqu’à **6** personnes, audio de tous vers tous (WebRTC en maillage, relais TURN de BeamCam).
- « Lancer l’appel » au-dessus de la saisie ; pastilles : qui est là, qui parle, qui a coupé son micro.
- **Spectateur** : rejoindre sans micro.
- **Live, en duo au plus** : deux personnes peuvent être en live en même temps (caméra, ou écran sur ordinateur), côte à côte (l’une sur l’autre en portrait) ; les autres regardent. Toujours de navigateur à navigateur.
- **Passer le relais** : en live, un clic sur la pastille de quelqu’un qui a un micro lui propose la scène ; il accepte (et autorise caméra ou écran) et l’on descend de scène.
- **Sonnerie** douce, une mélodie FM générée au hasard à chaque appel.
- **Notifications Web Push** (cloche 🔔, salon par salon) : prévenu même onglet fermé. Sur iPhone, seulement si le site est installé sur l’écran d’accueil.

## Le domaine perso (24/11/2026)

- Dans les options du lieu, « Nom de domaine » : un membre validé (ou l’admin) y écrit son domaine, qui doit pointer sur le serveur. Le lieu s’ouvre alors sur `https://son-domaine/`. Vider le champ le détache.
- Sur ce domaine, la marque « Sloubi.chat » et « Mon compte » ramènent au site ; « copier le lien » donne l’adresse du domaine.
- Un domaine qui arrive sur le serveur sans être rattaché à un lieu repart vers https://sloubi.chat.
- On redonne le mot de passe la première fois sur le domaine (les cookies sont propres à chaque adresse).
- Sur son domaine, le lieu s'installe comme sa propre application : le nom de la PWA, l'onglet et l'écran d'accueil portent le nom du lieu (« Tribu »), et l'icône est son initiale aux couleurs du décor.

## Les comptes (novembre 2026)

- **Discuter ne demande aucun compte.** Le compte sert à une chose : garder ses lieux par mot de passe (et donc fichiers, vocaux, appels).
- **Connexion par code e-mail**, comme sur Daily Jungle : on tape son adresse, on reçoit un code à 6 chiffres, valable 10 minutes. Rien à retenir.
- **Le mot de passe est réservé à l’admin et aux membres validés.** Dans les options du lieu, le bouton du mot de passe dit « Demander » tant qu’on n’est pas membre : si l’on n’est pas connecté, la connexion par code s’ouvre sur place, puis la demande part toute seule. Le lieu reste ouvert d’ici là ; « Annuler la demande » la retire.
- **L’admin valide** sur `/_/compte` : la liste des comptes, les demandes en attente d’abord, avec leurs lieux. Valider active les mots de passe demandés (les lieux se ferment aussitôt, tout le monde repasse par la porte) ; refuser les efface ; retirer l’accès empêche d’en poser d’autres mais laisse fermés les lieux déjà gardés. **L’admin ne voit jamais les mots de passe.**
- **Un seul admin** (l’adresse `ADMIN_EMAIL`, l’auteur), propriétaire de tous les lieux qui existaient avant les comptes : leurs anciens créateurs ne peuvent plus les régler.
- `/_/compte` montre aussi « Mes lieux » et leur état (gardé, en attente, ouvert).

## Le registre (`/_/`)

Tous les salons, les plus récemment actifs d’abord, avec leur décor. Un salon gardé apparaît avec un cadenas, sans ses chiffres.

## Le téléphone
- Clavier iOS : la page ne saute plus et ne défile plus. Seul le fil défile ; la mise en page suit `visualViewport`.
- Le bouton d’envoi garde le clavier ouvert ; glisser le fil vers le bas range le clavier.
- Champs de 16 px au moins (iOS zoome en dessous).
- PWA installable (manifeste et icônes ; elle démarre sur `/`).
