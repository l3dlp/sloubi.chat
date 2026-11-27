# Tests

## Go

```
go test -race ./...                       # tout
go test ./src/web -run TestUploads -v     # un seul
go vet ./...
```

Tout est dans `src/web/web_test.go`. `TestMain` se place à la racine du dépôt (le serveur lit `assets/`, `views/` et `forbidden.json` depuis le répertoire courant). Les tests ne doivent **jamais** écrire dans le `db/` du dépôt : `uploadRoot` et `vthumbRoot` sont remplacés par des dossiers temporaires.

Aides : `newTestServer` (serveur sur une base temporaire), `req` (requête avec cookie facultatif), `jsonField`, `dialRoom` / `wsClient` (WebSocket de test).

| Domaine | Tests |
|---|---|
| Noms de salon, `forbidden.json` | `TestCheckChannel`, `TestParseForbiddenRejectsEmpty`, `TestForbiddenExample` |
| Sloubis et base | `TestSloubiRule`, `TestChannelsAreIsolated`, `TestConcurrentSloubiCountsOnce`, `TestMigrationMovesLegacyMessagesToChante` |
| Post-it, routes, saisies | `TestPostit`, `TestRoutes`, `TestCleanText`, `TestNickname`, `TestExtractTags` |
| Anti-abus | `TestLimiter`, `TestClientKey`, `TestWSCap`, `TestSecurityHeaders` |
| Registre | `TestDirectory`, `TestDirectoryHidesLockedCounts`, `TestAgo` |
| Lieux, créateur, mot de passe | `TestPassword`, `TestRoomOwnerThemeAndPassword`, `TestThemesComplete` (chaque thème a son fichier FX) |
| Appels | `TestTurnCredentials`, `TestCallSignaling`, `TestCallLimits`, `TestCallStage`, `TestCallHandoff`, `TestSpectatorAndNickRequired` |
| Push | `TestPushCrypto` (vecteurs RFC 8291), `TestPushHosts`, `TestCallStartPushes` |
| Messagerie | `TestExtractLinks`, `TestLikesAndClicks`, `TestVideoLinks` |
| Fichiers | `TestClassify`, `TestJPEGOrientation`, `TestUploads`, `TestReclassify` |
| Comptes | `TestAdminClaimsExistingRooms`, `TestLoginCodes`, `TestPasswordNeedsApproval` |
| Domaines perso | `TestCustomDomains` (pose `siteHosts` à la main : par défaut il est nil et la fonction est coupée, car `httptest` envoie `Host: example.com`) |

Comptes : `newTestServer` crée l’admin `admin@exemple.fr` et remplace l’envoi des codes par `testMailer` (les codes atterrissent dans `sentCodes`). `login(t, r, email)` fait la vraie connexion par code ; `adminCookies(t, r)` donne la session de l’admin, nécessaire pour poser un mot de passe dans un test.

Piège : dans un salon verrouillé, toute requête de test doit porter le cookie d’accès (passer par `req` avec le cookie), sinon 403.

## Front

Pas de tests automatisés côté JS : on vérifie avec Playwright (Chromium) dans le bac à sable, captures à l’appui.

- **WebGL** : il faut `chrome-headless-shell` de Playwright avec `--use-angle=swiftshader --enable-unsafe-swiftshader`. Le Chromium du système n’a pas de WebGL en headless. Pour tester autre chose que la scène, on peut couper WebGL : la page bascule sur `body.no-webgl`.
- **Barres de défilement** : le headless de Playwright les cache. Utiliser le Chromium du système en `--headless=new`, sans `--hide-scrollbars`.
- **Heure du jour** : `?heure=14` (ou 6, 19, 23) force l’heure des scènes forêt et moulin.
- **Médias** : `--use-fake-ui-for-media-stream --use-fake-device-for-media-stream` pour la caméra et le micro. Pour le partage d’écran, remplacer `navigator.mediaDevices.getDisplayMedia` par une fonction qui renvoie `canvas.captureStream()` ; arrêter la piste simule « Arrêter le partage » du navigateur.
- **Comptes** : lancer le binaire avec `ADMIN_EMAIL=admin@test.fr` et sans envoi configuré ; les codes s’écrivent dans la sortie du serveur, que le script relit (`code de connexion pour <adresse> : <code>`).
- **Domaines perso** : `--host-resolver-rules=MAP chez-nous.test 127.0.0.1` dans Chromium, puis ouvrir `http://chez-nous.test:<port>/`.
- **Appels** : deux contextes de navigateur dans le même salon (même cookie d’accès), puis vérifier le `roster` et les pastilles.
- **Clavier iOS** : aucun émulateur ne le reproduit. Seul un vrai iPhone ou iPad fait foi.
- **Tactile** : `hasTouch` + `isMobile` dans le contexte Playwright ; le choix « Mon écran » doit disparaître (`pointer: coarse`).

Le serveur de test se lance avec `PORT=…  go run .` depuis la racine, sur une copie de `db/` si l’on ne veut pas toucher à la base locale.
