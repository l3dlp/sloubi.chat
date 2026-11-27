# Documentation de Sloubi.chat

Ce dossier rassemble tout ce qui ne tient pas dans le `README.md` (vitrine) ni dans `CLAUDE.md` (consignes pour l’assistant de code) : comment c’est construit, pourquoi, ce qui a cassé et ce qui reste à décider.

| Fichier | Contenu |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | Serveur Go fichier par fichier, client JS, modèle de données, flux (message, appel, fichier, push). |
| [API.md](API.md) | Toutes les routes HTTP et WebSocket, leurs entrées, sorties et codes d’erreur. |
| [FONCTIONNALITES.md](FONCTIONNALITES.md) | Ce que voit l’utilisateur : les six lieux, la messagerie, la médiathèque, l’enregistreur, les appels. |
| [SECURITE.md](SECURITE.md) | Modèle de menace, mesures en place, limites de débit, CSP, SSRF, fichiers. |
| [EXPLOITATION.md](EXPLOITATION.md) | Prod : systemd, Caddy, TURN, disque, déploiement, release GitHub. |
| [PIEGES.md](PIEGES.md) | Les bugs rencontrés et leur cause : à relire avant de toucher au CSS, au clavier iOS, aux fichiers ou aux médias. |
| [TESTS.md](TESTS.md) | Les tests Go, et comment tester le front (Playwright, WebGL, scrollbars). |
| [HISTORIQUE.md](HISTORIQUE.md) | Le fil des demandes (novembre 2026) et ce qui a été fait pour chacune. |
| [A-FAIRE.md](A-FAIRE.md) | Le travail en cours, les décisions en attente (admin, abonnements, quota) et la dette connue. |

`TODO.md` à la racine reste la feuille de route historique (cases cochées) ; `A-FAIRE.md` en est la version courte et à jour.
