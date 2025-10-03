# Sloubi.chat — le chat médiéval indépendant

Meta a fermé les discussions de groupe ?  
On ne va pas pleurer, on va forger notre propre taverne.

**Sloubi** est une réponse directe à la fermeture des groupes de discussion par Meta.  
Plutôt que de se laisser enfermer dans des walled gardens qui tirent la prise quand ça les arrange, on s’est construit un **chat indépendant, libre et fun**, centré autour d’un seul mot magique : **sloubi**.

Ici, pas d’algos, pas de pub, pas de flicage. Juste un compteur communautaire qui monte chaque fois que quelqu’un lâche “sloubi” dans la conversation.

---

## Fonctionnalités

- **Chat en temps réel** :  
  REST pour poster, WebSocket pour recevoir.  
- **Sloubi compteur** :  
  Tape “sloubi” → le serveur incrémente le compteur et le dernier sloubi est mis en avant.  
- **Markdown support** :  
  Tu peux formatter ton message en gras, italique, listes…  
- **GIF selector** :  
  Choisis un GIF (par défaut taggé *Kaamelott*, parce qu’on a des références solides).  
- **Emoji picker** :  
  Récents + recherche, insertion directe dans le texte.  
- **Sécurité à la hache** :  
  - Markdown rendu et *sanitizé* côté serveur.  
  - GIFs validés localement dans `/assets/gifs`.  
  - Nicknames propres (taille et caractères filtrés).  
  - Rate-limit par IP.  
- **PWA installable** :  
  Manifest, icônes, offline-ready.  

---

## Structure

```
sloubi/
├─ main.go # serveur Gin-Golang (REST + WS + SQLite)
├─ go.mod
├─ db/chat.sqlite # base SQLite (auto-créée)
├─ views/
│ └─ index.html # UI principale
└─ assets/
├─ app.js
├─ styles.css
├─ manifest.json
├─ icon-192.png
├─ icon-512.png
└─ gifs/ # dossiers GIF
```


---

## Installation

```
go mod tidy
go run .
```


Tu peux aussi builder une image Docker :

```
docker build -t sloubi .
docker run -p 8080:8080 sloubi
```



---

## Stockage

- Les messages sont enregistrés en **SQLite** (`db/chat.sqlite`).  
- Le serveur calcule les sloubi (le client ne décide rien).  
- `/last-sloubi.json` expose le dernier sloubi → affiché en héros à l’arrivée.  
- WS diffuse backlog (100 derniers) puis live.  

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

- Pas de tracking tiers.  
- Pas de pub.  
- Pas d’export des données utilisateurs.  
- Auth simplifiée : tu choisis ton pseudo → stocké localement.  
- Si tu veux durcir : tu peux brancher OAuth (Google/Apple/GitHub) + JWT côté backend.  

---

## Roadmap

- [x] REST + WS avec Gin  
- [x] SQLite pour la persistance  
- [x] Markdown safe (goldmark + bluemonday)  
- [x] Compteur sloubi  
- [x] GIF selector (Kaamelott par défaut)  
- [x] Emoji picker (recherche + récents)  
- [x] PWA installable  
- [ ] Rooms / salons multiples  
- [ ] Auth via OAuth  
- [ ] Hébergement fédéré (serveurs Sloubi interconnectés)  

---

## Licence

MIT.  

Prends, copie, hacke.  

Fais tourner ton propre sloubi-server et arrêtons d’attendre qu’un géant décide si on a encore le droit de parler.
