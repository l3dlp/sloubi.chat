# Sécurité

## Principes
- **Pas de compte pour discuter**, pas de pistage, pas de tiers. Le compte (une adresse e-mail) ne sert qu’à garder un lieu. Le pseudo et le `clientId` restent dans le navigateur ; le `clientId` n’est jamais renvoyé aux autres (`json:"-"`).
- **On ne compte que des totaux** : cœurs par message, clics par lien. Jamais qui.
- **Le serveur fait foi** : sloubis, horodatage, classement des fichiers, droits d’accès.
- **Aucune requête du navigateur vers un tiers** : polices, Three.js, miniatures vidéo, tout est servi par le site (`img-src 'self'`).

## Accès aux salons
- Mot de passe : PBKDF2-SHA256, 210 000 itérations, sel aléatoire. Essais limités par IP (`lockLimit` : 0,2/s, rafale 5, ban de 15 min après 10 échecs).
- Cookie d’accès `sloubi_k_<chan>` = HMAC(`meta.cookie_secret`, chan + pw_hash) : HttpOnly, 30 jours, révoqué dès que le mot de passe change.
- Clé de créateur : aléatoire, montrée une seule fois, stockée en SHA-256.
- `server.enter` garde **toutes** les routes de salon et le WS ; les fichiers, le TURN, les push et les appels exigent en plus un salon verrouillé.
- Un changement de thème ou de mot de passe évince le salon (`Hub.Evict`).
- Le registre `/_/` masque les chiffres des salons verrouillés.

## Comptes
- Connexion sans mot de passe : code à 6 chiffres (`crypto/rand`) envoyé par e-mail, valable 10 min, un renvoi par minute et par adresse, 5 essais puis le code tombe. En base, seulement un HMAC du code (`otp_codes.code_hash`), comparé en temps constant.
- Débit : `codeLimit` (1 code / 30 s par IP, rafale 4) contre l’envoi de courriels en masse ; `lockLimit` sur la vérification.
- Session : cookie `sloubi_session` signé HMAC-SHA256 (`meta.cookie_secret`), HttpOnly, SameSite=Lax, `Secure` derrière HTTPS, 90 jours. Le compte est relu à chaque requête : refuser ou retirer prend effet tout de suite.
- POST en `application/json` seulement : un formulaire d’un autre site ne peut pas appeler l’API sans preflight CORS (même protection que le reste).
- L’admin ne voit que les adresses et les lieux, jamais un mot de passe : la demande ne garde que le hachage PBKDF2 (`pw_pending`).
- `retirer` ne rouvre jamais un lieu gardé : ses messages et ses fichiers resteraient sinon lisibles par tous.
- `?retour=` de `/_/compte` n’accepte qu’un chemin de salon (`/nom`) : pas de redirection ouverte.
- L’adresse admin vient de l’environnement (`ADMIN_EMAIL`, `.env` ignoré par git), jamais du dépôt.

## Domaines perso
- Seuls les membres validés et l’admin rattachent un domaine : chaque domaine coûte un certificat, et le proxy n’en demande que pour un domaine rattaché (`/_/domaine`, répondu aux seuls clients locaux).
- Un domaine ne peut pas être une IP, localhost, ni sloubi.chat ou l’un de ses sous-domaines ; un domaine ne sert qu’un lieu (index unique).
- Domaine inconnu : redirection vers le site, jamais une page servie. Les redirections vont vers `SITE_URL` (fixé par la configuration), jamais vers un hôte fourni par la requête.
- Le WebSocket compare déjà `Origin` au `Host` demandé : il marche sur le domaine perso sans s’ouvrir aux autres sites.

## Saisies
- `application/json` exigé, corps de 4 Ko au plus ; message 500 caractères, post-it 280, pseudo 2 à 24.
- `cleanText` : NFC, sans caractères de contrôle ni de format (bidi, largeur nulle ; le ZWJ des emojis est gardé), 2 diacritiques par lettre au plus (pas de zalgo).
- `cleanNickname` : alphabet latin, chiffres ASCII, espace et `_ - . '` ; refuse au lieu de retirer (pas de sosies cyrilliques).
- Noms de salon : regex stricte et `forbidden.json` après normalisation (minuscules, séparateurs retirés, leet → lettres). Le fichier n’est jamais servi ; s’il manque ou est cassé au démarrage, le serveur refuse de démarrer, et un fichier cassé en cours de route garde la liste précédente.
- Le texte est toujours rendu en `textContent` ; les liens sont construits par le DOM (`rel="noopener noreferrer nofollow ugc"`, `target="_blank"`).

## Limites de débit

Clé : l’IPv4, ou le préfixe /64 en IPv6 (`clientKey`). Gin ne croit `X-Forwarded-For` que de 127.0.0.1/::1 (Caddy).

| Limiteur | Débit | Rafale | Ban |
|---|---|---|---|
| `postLimit` (messages) | 1/s | 5 | — |
| `postitLimit` | 1 / 3 s | 3 | — |
| `checkLimit` (`/api/check`) | 4/s | 10 | 10 min après 30 refus |
| `wsLimit` (ouvertures de WS) | 1 / 2 s | 10 | — |
| `createLimit` (nouveaux salons) | 10 / h | 10 | — |
| `lockLimit` (mots de passe, réglages) | 0,2/s | 5 | 15 min après 10 échecs |
| `anyLimit` (`floodGuard`, toutes requêtes) | 30/s | 120 | — |
| `reactLimit` (cœurs, clics) | 3/s | 20 | — |
| `uploadLimit` (fichiers) | 2/s | 40 | — |
| `codeLimit` (codes par e-mail) | 1 / 30 s | 4 | — |

Plus : 12 WebSockets simultanés par clé (`maxWSPerKey`), 5000 au total ; trames WS entrantes de 32 Ko avec un seau à jetons par client.

## Transport et en-têtes
- `http.Server` : en-têtes en 5 s (pas de slowloris), lecture 15 s, écriture 30 s, inactivité 120 s, en-têtes de 16 Ko. Les envois et téléchargements de fichiers prolongent leurs propres délais (`http.ResponseController`).
- WebSocket : même origine seulement.
- CSP : `default-src 'none'; script-src 'self'; connect-src 'self'; img-src 'self' data:; media-src 'self' blob:; style-src 'self'; font-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`. **Aucun style ni script inline** : `hidden` ou CSSOM, jamais `style="…"`. Sans `media-src`, aucune vidéo ni aucun son ne joue (bug rencontré).
- `Permissions-Policy` : `camera=(self), microphone=(self), display-capture=(self)`, et `geolocation`, `payment`, `usb`, `serial`, `browsing-topics` fermés.
- HSTS (1 an, sous-domaines), `X-Frame-Options: DENY`, `nosniff`, COOP et CORP `same-origin`, `Referrer-Policy: same-origin`.
- `TestSecurityHeaders` vérifie la CSP et la `Permissions-Policy`.

## Fichiers partagés
- Seulement dans les salons verrouillés, seulement avec accès.
- Stockés sous un id aléatoire (jamais le nom d’origine) dans `db/uploads/<chan>/`.
- Type déterminé **par les octets** (`http.DetectContentType`), pas par le nom ; HTML et SVG sont toujours des documents.
- Servis inline seulement pour une liste blanche de médias ; tout le reste en `Content-Disposition: attachment`. Toujours une CSP `default-src 'none'; … sandbox` et `nosniff` : un fichier piégé ne s’exécute pas dans l’origine du site.
- Quota par salon et réserve disque (5 Go) ; réservations pour que des envois parallèles ne dépassent pas.
- Miniatures limitées à 50 Mpx (pas de bombe de décompression en mémoire).
- **Ouvert** : les photos gardent leurs métadonnées EXIF, position GPS comprise. Pas de suppression de fichier.

## SSRF
Le serveur ne va chercher que ce qui est en liste blanche :
- **Push** : `pushHostAllowed` (FCM, Mozilla, Windows, Apple) ; un abonnement vers un autre hôte est refusé.
- **Vidéos** : `videoHosts` (YouTube, Vimeo, Dailymotion et leurs CDN de miniatures), vérifié aussi à chaque redirection ; réponses plafonnées (oEmbed 64 Ko, miniature 3 Mo).
- `vthumb` ne sert que les vidéos citées dans le salon : ce n’est pas un proxy ouvert.
- **Les autres liens n’ont pas d’aperçu, exprès** : aller chercher des URL fournies par les membres serait une SSRF.

## Appels
- Signalisation relayée seulement entre participants, au seul destinataire (`to`).
- Identifiants TURN éphémères (6 h, HMAC du secret de coturn), délivrés seulement avec l’accès au salon. Le secret est lu dans le fichier de BeamCam (`TURN_SECRET_FILE`), jamais copié.
- Le flux audio et vidéo va de navigateur à navigateur (chiffré DTLS-SRTP) ; le serveur ne le voit pas.

## Exploitation
- Service systemd durci (`deploy/hardening.conf`) : `www-data`, système en lecture seule sauf `db/`, aucune capacité, appels système filtrés, 512 Mo de mémoire au plus. Score `systemd-analyze security` de 1,2.
- Le serveur n’écoute que sur 127.0.0.1 par défaut (`HOST` pour changer).
- `govulncheck` dans `bin/build.sh` : le build échoue sur une vulnérabilité atteignable.
