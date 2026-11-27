# Historique

Les grandes étapes, puis le fil des demandes de novembre 2026 (sessions de travail avec Claude Code) et ce qui a été fait pour chacune. Les citations sont les mots de l’auteur.

## Grandes étapes

| Date | Étape |
|---|---|
| — | Prototype : une seule taverne, le compteur de sloubis (`7babd1a`, `a15c878` « Sloubi indépendant »). |
| 31/10/2026 | Multi-salon `sloubi.chat/<chan>`, accueil, post-it, `forbidden.json`, durcissement. |
| 01/11/2026 | Audit : timeouts, IPv6 /64, plafonds de WS, texte libre nettoyé, CSP sans inline, service systemd durci (« Mode Forteresse », `d565542`). |
| 14/11/2026 | Trois lieux (taverne, forêt, moulin), créateur, mot de passe. |
| 19/11/2026 | Appels de groupe, live, relais, sonnerie, Web Push, spectateurs. |
| 20/11/2026 | Messagerie privée : bulles, barre d’icônes, fichiers, médiathèque, enregistreur, liens. Trois thèmes 2D. Nouvelle landing. |
| 21/11/2026 | **Release v1.0.0** (`307ad5a`) « Sloubi 1.0 : une messagerie privée, en six lieux ». Puis partage d’écran. |
| 24/11/2026 | Domaines perso : un lieu s’ouvre sur son propre domaine, les domaines inconnus repartent vers sloubi.chat. |
| 22/11/2026 | Dossier `docs/`. Comptes : connexion par code e-mail, admin unique, mot de passe réservé aux membres validés ; quota 1001 Go. |

## Le fil des demandes (novembre 2026)

1. **« le créateur du lieu [doit avoir] la possibilité de mettre un mot de passe ; […] 3 thèmes + app »** : la taverne (sloubis), la forêt nourricière en permaculture (« chaque microbiote est une encyclopédie », symbolique), le moulin au bord de la rivière. → `rooms.go`, clé de créateur, PBKDF2, cookie HMAC, `forest-fx.js`, `mill-fx.js`, `fx-kit.js`.
2. **« tu suis l’heure qu’il est dans le moulin ? »** puis **« pour les 2 »** → `daylight()`, `skyColor()`, `?heure=`.
3. **« tu peux faire mieux pour le rendu de l’eau ? »** → second rendu depuis une caméra miroir, distorsion par vagues, Fresnel, reflets ; ciel en dôme shader.
4. **Appels dans un groupe privé**, en reprenant BeamCam (MIT, même auteur) : « 5/6 max », « réservés aux salons avec mot de passe », coturn de BeamCam sur le même serveur. → `calls.go`, `call.js`, `deploy/turn.conf`.
5. **« un des interlocuteurs [peut] passer en live et les autres le suivent »**, un par un → `Hub.stage`, transceiver vidéo négocié d’avance, `replaceTrack`.
6. **« webpush une info + sonnerie modulaire aléatoire mais douce »** → `push.go` (RFC 8291/8292 en bibliothèque standard), `ring.js`, `sw.js`.
7. **Rejoindre en simple spectateur**, et **« un anonyme devrait changer de pseudo AVANT de pouvoir écrire »** → `listen`, `nick-required`, `400 nickname required`, `nickMode`.
8. **En live, cliquer sur un connecté avec micro pour lui passer le live** → `handoff`, `Hub.relay`, `handoff-decline`.
9. **iPad + Brave : « Sans micro » et « Rejoindre » sans effet** → cause côté Caddy (`/api*` limité à une IP), corrigée par l’auteur.
10. **« le mot de passe doit être généralisé »**, demandé **dès l’accueil** → `/api/check` renvoie `access`, champ mot de passe dans `home.js`.
11. **Réécrire la landing, « BEAUCOUP plus sobre », ambiance NADAPROD**, licence MIT, histoire du nom (« parti d’un râle devant la suppression d’une fonction de discussion à plusieurs sur Facebook, une chouette communauté disparue dans la foulée ») → `home.html`, `home.css`, `marker.css`, polices Permanent Marker et Archivo.
12. **Android : « le serveur ne répond pas » en boucle** → même cause Caddy. « j’ai tout vérifié, merci ».
13. **Réglages instantanés** : un clic sur un thème s’applique ; pseudo et mot de passe ont un bouton « changer » > « en cours… » > « changé » → `runButton`, `postChange`.
14. **Trois thèmes « There Is No Game 2 »-spirit** : cyberpunk (« très 80s », « cousin du punk », « collages »), kawaii, BD franco-belge ; landing mise à jour → `fx2d.js`, `cyber-fx.js`, `kawaii-fx.js`, `bd-fx.js`.
15. **`/awwwards` : « explose la navigation […] en un objet visitable »** → un dé 3D en CSS, puis un dé « plus réaliste sur fond blanc », puis **« oublie toute cette histoire de dé »** : retiré.
16. **Messenger privé** : liens en `_blank`, totaux de clics et de cœurs seulement, cœur sur tout message, menu Discussion / Favoris / Images / Vidéos / Documents, envoi de fichiers dans les salons à mot de passe (10 Go), classement automatique ; inspiré de `/intelligences/lh.family/tchat` et `tchat-v2` → `reactions.go`, `files.go`, `library.js`.
17. **« les compteurs sont anecdotiques, comme un score en haut à droite » ; « où est le fil de discussion » ; « compris par la mamie de 72 ans »** → refonte : barre du haut, fil en panneau, bulles, avatars, jours.
18. **Clavier iOS, « les sauts et les scrolls inutiles »** → `viewport.js`, `body.room` fixe, `--vvh`.
19. **« pourquoi pas d’aperçu pour les png ? »** → ordre de `classify` (C2PA), `reclassify`, fond blanc des miniatures.
20. **Un « + » pour enregistrer un audio ou une vidéo**, compressés par le navigateur → `recorder.js`.
21. **« le menu au clic c’est trop chiant » → barre d’icônes**, à gauche en paysage, en haut en portrait → `.app-nav`.
22. **Vidéothèque avec les liens YouTube & co** (visuel + `_blank`), **les autres liens dans Documents** → `videolinks.go`.
23. **« on a oublié de faire de belles scrollbars »** → `--sb-*` par thème, `@supports` pour Firefox.
24. **Landings à jour, dé oublié** → fait.
25. **« commit and make it a full release »** → v1.0.0, archives amd64/arm64, `SHA256SUMS`, binaire retiré de git.
26. **« dans les "+" […] un rapide partage d’écran vidéo + micro pour montrer qq chose sans la cam »** → `recorder.js` (choix « Mon écran + micro », pastille compacte pendant l’enregistrement), testé avec un canvas à la place de `getDisplayMedia`.
27. **« on passe le côté de base à 1001 Go PAR CONTRE seul l’admin et les gens avec un abonnement auront un mot de passe, avec moi comme premier user, seul admin, et propriétaire des canaux existants »** Précisions de l’auteur : « oui 1001 Go, on va déléguer en CDN bunny.net avant ouverture » ; « pour l’instant juste un accès carré, l’admin valide qui a accès, l’utilisateur peut juste faire la demande quand il fixe son passe (que je ne vois pas) » ; « le même système de connexion OTP que […] dailyjungle.net ». → `accounts.go`, `mail.go`, `login.js`, `/_/compte`, `pw_pending`, `ensureAdmin`.
28. « tu mets le max en .md dans un dossier ./docs dans le projet » → ce dossier.
29. « je vais raccorder des domaines au même bot […] tu peux donner une URL à ton sloubi en paramétrant le nom du domaine dans les paramètres du canal, et si l’app est appelée par un autre domaine que sloubi.chat on checke si le canal existe, il est ouvert sur la route /, sinon on renvoie vers https://sloubi.chat » ; « je m’occupe du serveur, gère juste le go » → `domains.go`, champ « Nom de domaine » dans les options.
30. « le mode à 2 en live max pour les appels vidéo en restant peer-to-peer » → `maxLive = 2`, vignettes côte à côte. « tous mes messages sont sur la droite […] même les messages avec des PJ ; les PJ […] en height:48px […] les flèches gauche droite parcourent tout le flux » → `sloubi:sent`, `.file-thumb`, visionneuse sur tout le fil. « on supprime TOUS les messages à la con […] et on indique seulement qui rentre et qui sort » → `presence.go`.

## Choix de conception notables
- **Pas de renégociation WebRTC pour le live** (repris de BeamCam) : une place vidéo vide est négociée d’avance, et passer en live est un `replaceTrack`.
- **Web Push sans bibliothèque** : la crypto de la bibliothèque standard Go suffit, et on évite une dépendance sur un chemin sensible.
- **Le serveur ne dit jamais qui a écrit quoi** à un autre client ; « mes messages » est un souvenir du navigateur.
- **Pas d’aperçu des liens ordinaires** (SSRF) ; seules trois plateformes vidéo en liste blanche.
- **Tout est auto-hébergé**, aucune requête vers un tiers depuis le navigateur.
