/* viewport.js — le clavier des téléphones, surtout sur iOS. Safari ne rétrécit
   pas la page quand le clavier s'ouvre : il la fait glisser, la barre du haut
   disparaît, et elle ne revient pas toujours à sa place. On suit donc la partie
   vraiment visible (visualViewport) : la page ne défile jamais, l'écran du salon
   prend exactement la hauteur visible, la saisie reste collée au clavier. */

'use strict';

const root = document.documentElement;
const vv = window.visualViewport;
let frame = 0;

function apply() {
  frame = 0;
  const h = vv ? vv.height : window.innerHeight;
  const top = vv ? vv.offsetTop : 0;
  root.style.setProperty('--vvh', `${Math.round(h)}px`);
  root.style.setProperty('--vvt', `${Math.round(top)}px`);
  // iOS a fait défiler la page derrière le clavier : on la remet à zéro (seul le fil défile)
  if (window.scrollY || window.scrollX) window.scrollTo(0, 0);
  const open = window.innerHeight - h > 120;
  if (open !== document.body.classList.contains('kb-open')) document.body.classList.toggle('kb-open', open);
  window.dispatchEvent(new CustomEvent('sloubi:viewport', { detail: { height: h, keyboard: open } }));
}

const schedule = () => { if (!frame) frame = requestAnimationFrame(apply); };
vv?.addEventListener('resize', schedule);
vv?.addEventListener('scroll', schedule);
window.addEventListener('resize', schedule);
window.addEventListener('scroll', () => { if (window.scrollY) window.scrollTo(0, 0); }, { passive: true });
// à la fermeture du clavier, iOS rend la place avec un temps de retard : on revérifie
document.addEventListener('focusout', () => setTimeout(apply, 120));
apply();
