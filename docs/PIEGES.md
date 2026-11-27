# Pièges rencontrés

Chaque entrée : le symptôme, la cause, le correctif. À relire avant de retoucher la zone concernée.

## CSS et mise en page

- **Un élément fixe qui ne se fixe pas.** Le dé partageait la classe `.hero` ; l’animation du héros posait un `transform`, qui crée un bloc conteneur et piège `position: fixed`. → Classe dédiée. Règle générale : un ancêtre avec `transform`, `filter` ou `will-change` casse le `fixed`.
- **Grille du salon décalée** par le panneau des microbiotes. → `.room-shell` en grille à zones nommées (bar / nav / main).
- **Débordements horizontaux sur mobile.**
  - Un `<fieldset>` refuse de rétrécir : `min-width: 0`.
  - Des champs dans un flex : `width: 0; flex: 1 1 0`.
  - `.room-bottom` : `grid-template-columns: minmax(0, 1fr)`.
  - `.thread` : `min-width: 0`.
- **Images déformées** dans les bulles. → Variable CSS `--ratio` posée en CSSOM, plus `aspect-ratio`.
- **Lecteur audio écrasé** dans une bulle étroite. → Largeur fixe de 300 px.
- **Couleurs sombres écrasées** : des valeurs de base déclarées *après* les surcharges de thème. → L’ordre de la cascade compte, les surcharges viennent en dernier.
- **Barres de défilement ignorées dans Chrome** : dès que `scrollbar-color` ou un `scrollbar-width` non `auto` s’applique, Chrome ignore les `::-webkit-scrollbar`. → `scrollbar-color` seulement dans `@supports (-moz-appearance: none)` (Firefox).
- **Pas de style inline** : la CSP refuse `style="…"`. Utiliser l’attribut `hidden`, une classe, ou `el.style.x = …`.

## Interactions

- **Clics avalés** sur les pastilles d’appel : la rangée était reconstruite sous le curseur, et un survol qui ajoutait du texte la faisait glisser. → Délégation des clics sur la liste, reconstruction seulement si la signature change.
- **Entrée sans effet** dans un formulaire à deux champs sans bouton d’envoi (le navigateur ne soumet plus implicitement). → Entrée gérée en JS sur chaque champ.
- **Course au rechargement** après un réglage : le serveur évince le salon *avant* de répondre, donc le socket se ferme avant la réponse HTTP. → `reloadDelay` posé avant la requête (`postChange`).
- **Le Post-it traité comme une vue** de la barre d’icônes. → Sélecteur `.nav-item[data-view]` seulement.
- **Post-it par-dessus la barre** → `clamp()` de sa position.
- **Clavier iOS** : iOS fait défiler la page au lieu de la redimensionner. → `body.room` en `position: fixed`, seul `#chatMessages` défile, la hauteur suit `visualViewport` (`--vvh`, `--vvt`), champs de 16 px au moins. Le bouton d’envoi garde le focus (`touchend` + `mousedown` avec `preventDefault`).
- **WebSocket et bfcache** : un socket qui survit à la navigation bloque le navigateur plusieurs secondes. → Fermer sur `pagehide`, rouvrir sur `pageshow`.

## Fichiers et médias

- **Aucune vidéo, aucun son ne jouait** : la CSP n’avait pas de `media-src`, donc `default-src 'none'` s’appliquait. → `media-src 'self' blob:` (`blob:` pour la relecture avant envoi). `TestSecurityHeaders` le vérifie.
- **PNG rangés en documents, sans aperçu** : un manifeste C2PA (images générées) contient la chaîne `<svg`, et le test « est-ce du SVG ? » passait avant la signature PNG. → `classify` regarde d’abord la signature binaire ; `reclassify` corrige les fichiers déjà rangés au démarrage.
- **Miniatures noires** des PNG transparents (JPEG n’a pas d’alpha). → Transparence posée sur du blanc.
- **Orientation EXIF fausse** : la borne d’un segment était mal calculée. → `i + 2 + size`.
- **Message vocal rangé en vidéo** : un WebM ou MP4 audio seul a la signature d’une vidéo. → L’extension (`.weba`, `.m4a`) tranche (`audioExt`), et l’enregistreur la pose.
- **Gzip et gros fichiers** : l’enveloppe gzip de Gin ne déballe pas l’écrivain d’origine, donc `http.ResponseController` ne peut plus prolonger les délais. → `/api/*/files` exclu de gzip.
- **Envois coupés à 15 s** : délai de lecture du serveur. → `longRequest` prolonge les délais pour les envois et téléchargements.

## Thèmes

- **Kawaii** : `Array.from({length}, fn)` ne passe pas le tableau à `fn` ; une fonction qui lisait le tableau en construction plantait. → Constante `MOCHIS`.
- **Kawaii** : l’étiquette du bouton passait sous la tête. → Tête placée avant, bouton en pilule.
- **Texte illisible en plein jour** sur la forêt et le moulin. → `body[data-daylight]`, que `themes.css` utilise pour foncer le texte.

## Prod et outillage

- **« Le serveur ne répond pas » sur un seul appareil** : Caddy filtrait `/api*` par IP. Voir [EXPLOITATION.md](EXPLOITATION.md#caddy).
- **Tests qui écrivaient dans `db/`** du dépôt. → `uploadRoot` et `vthumbRoot` sont des variables, remplacées dans les tests.
- **L’outil refuse `rm -rf $variable`** (garde-fou de l’assistant). → Chemins littéraux, ou dossiers neufs sans suppression.
- **`rm` est un alias interactif** sur le serveur : utiliser `\rm -f` dans les scripts.
- **Requête de test refusée (403)** dans un salon verrouillé : il faut le cookie d’accès.
