# À faire

Mis à jour le 22/11/2026. `TODO.md` à la racine garde la feuille de route détaillée et cochée.

## Mise en service des comptes (fait le 22/11/2026, pas encore en prod)
- **Configurer `.env` en prod** (modèle : `.env.example`) :
  - `ADMIN_EMAIL` = l'adresse de l'auteur ;
  - l'envoi des codes : les identifiants SMTP ou Brevo de Daily Jungle peuvent servir ;
  - `MAIL_SENDER` : un alias vérifié côté Gmail, ou un expéditeur validé chez Brevo.
- Au premier démarrage avec `ADMIN_EMAIL`, l'admin devient propriétaire des **12 lieux existants**. Les créateurs de `tribu` et `clh` (gardés) et ceux de `l3dlp`, `chaise-pliante` et `testos` perdent leurs réglages : ils pourront demander un compte, mais **aucun outil ne transfère encore un lieu** de l'admin vers un membre.
- Sans `.env`, rien ne casse : pas d'admin, les codes s'écrivent dans le journal, et personne ne peut poser de nouveau mot de passe sans validation.

## Comptes : suites possibles
- Transférer un lieu de l'admin vers un membre (depuis `/_/compte`).
- Supprimer son compte (et ce que deviennent ses lieux).
- Prévenir l'admin d'une nouvelle demande (e-mail ou Web Push), et le membre de la décision.
- Abonnement payant : pour l'instant, « juste un accès carré » validé à la main.
- La clé de créateur reste utile pour le décor des lieux sans compte ; la réduire à un mode « invité » si elle embrouille.

## Domaines perso
- Côté proxy (l’auteur) : TLS à la demande branché sur `/_/domaine`.
- Domaines accentués : à écrire en punycode (`xn--…`) ; convertir automatiquement si besoin (`golang.org/x/net/idna`).
- PWA : le manifeste est le même partout (nom « Sloubi ») ; un manifeste par domaine serait plus joli.
- Montrer le domaine dans l’annuaire `/_/` et dans « Mes lieux ».

## Fichiers : CDN
- Quota passé à **1001 Go par salon** (décision du 22/11/2026). Le disque n'a que 23 Go libres : `diskReserve` (5 Go) reste le vrai garde-fou.
- **Avant l'ouverture : déléguer le stockage à bunny.net** (Storage + CDN, URLs signées pour garder les fichiers privés au salon).

## Exploitation
- **Disque à 94 %** (23 Go libres sur 345). Agrandir, ou mettre les uploads sur un autre volume (`ReadWritePaths` à ajouter), avant d’ouvrir les fichiers largement.
- Redéployer la prod (`bin/build.sh`, lancé par l’auteur) après le prochain commit.
- Installer `govulncheck` sur le serveur, ou l’accepter absent de `bin/build.sh`.
- Retester l’appel sur iPad et Android depuis la correction de Caddy.
- La capture `Capture d’écran 2026-11-20 à 22.30.55.png` traîne, non suivie, à la racine du dépôt : à supprimer ou à ranger.

## Fichiers
- Supprimer un fichier (le créateur ou l’auteur) ; décider ce que deviennent les fichiers d’un salon qui perd son mot de passe.
- Retirer les métadonnées EXIF (dont le GPS) des photos à l’envoi.
- Miniatures des vidéos côté serveur (il faudrait ffmpeg) ; pour l’instant, le navigateur montre la première image.

## Appels
- Débit vraiment adaptatif (celui de BeamCam : `getStats()` toutes les 2 s) au lieu de plafonds fixes.
- Une coupure du WebSocket raccroche l’appel. Il faudrait garder les connexions WebRTC et renégocier à la reconnexion.
- Notifications sur iPhone seulement pour le site installé ; liée à la question de la PWA par salon.

## Divers
- Purger ou archiver les salons inactifs.
- PWA par salon (manifeste dynamique) ou garder l’accueil comme `start_url`.
- Clé de créateur perdue : aucun moyen de la retrouver. Le compte admin pourrait régler ça.
- Contourner « pas deux sloubis d’affilée » en vidant son `localStorage` reste possible.
- Dockerfile : en faire un, ou acter qu’il n’y en aura pas.
- Roadmap du README : Markdown sûr (goldmark + bluemonday), sélecteur de GIF, emojis, OAuth, fédération.
