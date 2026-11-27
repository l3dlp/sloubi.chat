// fx.js — choisit le décor animé du salon. Sur ordinateur : la scène du thème (Three.js
// ou canvas 2D). Sur téléphone et tablette tactile : un simple fond en shader aux couleurs
// du thème (shader-bg.js), parce que les scènes y saccadent et se voient à peine sous le fil.
// ?3d dans l'URL force la scène, ?leger force le fond simple (pour essayer).
const fx = document.getElementById('fxLoader')?.dataset.fx || '';
const q = new URLSearchParams(location.search);
const touch = matchMedia('(pointer: coarse) and (hover: none)').matches;
if ((touch || q.has('leger')) && !q.has('3d')) {
  document.body.classList.add('fx-light');
  import('./shader-bg.js');
} else if (/^[a-z0-9-]+\.js$/.test(fx)) {
  import(`./${fx}`);
}
