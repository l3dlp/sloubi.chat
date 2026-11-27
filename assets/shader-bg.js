// shader-bg.js — le fond des téléphones : deux couleurs du thème qui ondulent lentement,
// une lueur de l'accent, un vignettage. Un seul shader sans boucle, dessiné à 1/6 de la
// résolution (le navigateur l'agrandit en douceur), 20 images par seconde au plus, à
// l'arrêt quand l'onglet est caché, une seule image si l'on préfère moins d'animation.
const canvas = document.getElementById('smoke');
const PALETTES = { // fond, second plan, accent (themes.css)
  taverne: ['#140b07', '#3b1a0b', '#ff8a3d'],
  foret: ['#08130c', '#123320', '#6ee0a6'],
  moulin: ['#0d1a24', '#1a3a50', '#7cc8e4'],
  cyber: ['#0d0614', '#2c0b40', '#00f0ff'],
  kawaii: ['#ffe4f1', '#e6dcff', '#9b7bff'],
  bd: ['#fdf6e3', '#f2e4c2', '#3e6990'],
};
const pal = PALETTES[document.body.dataset.theme] || PALETTES.taverne;
const rgb = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255);
const lightTheme = rgb(pal[0]).reduce((a, b) => a + b) > 1.5;

const gl = canvas?.getContext('webgl', { alpha: false, antialias: false, depth: false, stencil: false, powerPreference: 'low-power', preserveDrawingBuffer: false });
if (!gl) {
  document.body.classList.add('no-webgl'); // le fond CSS du thème prend le relais
} else {
  const vs = 'attribute vec2 p;void main(){gl_Position=vec4(p,0.,1.);}';
  const fs = `precision mediump float;
uniform vec2 r;uniform float t;uniform vec3 c0,c1,c2;uniform float lt;
void main(){
  vec2 uv=gl_FragCoord.xy/r;
  vec2 p=uv*vec2(r.x/r.y,1.);
  float a=sin(p.x*2.1+t*.07)+sin(p.y*1.7-t*.05)+sin((p.x+p.y)*1.3+t*.04);
  vec3 col=mix(c0,c1,smoothstep(-1.6,1.6,a)*.85);
  vec2 o=vec2(.5+.35*sin(t*.031),.35+.25*cos(t*.043));
  float g=1.-smoothstep(0.,.75,length(p-o*vec2(r.x/r.y,1.)));
  col+=c2*g*g*mix(.16,.10,lt);
  float v=smoothstep(1.15,.25,length(uv-.5)*1.35);
  col*=mix(mix(.62,.9,lt),1.,v);
  gl_FragColor=vec4(col,1.);
}`;
  const sh = (type, src) => { const s = gl.createShader(type); gl.shaderSource(s, src); gl.compileShader(s); return s; };
  const prog = gl.createProgram();
  gl.attachShader(prog, sh(gl.VERTEX_SHADER, vs));
  gl.attachShader(prog, sh(gl.FRAGMENT_SHADER, fs));
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    document.body.classList.add('no-webgl');
  } else {
    gl.useProgram(prog);
    gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW); // un triangle couvre l'écran
    const loc = gl.getAttribLocation(prog, 'p');
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
    const u = (n) => gl.getUniformLocation(prog, n);
    gl.uniform3fv(u('c0'), rgb(pal[0]));
    gl.uniform3fv(u('c1'), rgb(pal[1]));
    gl.uniform3fv(u('c2'), rgb(pal[2]));
    gl.uniform1f(u('lt'), lightTheme ? 1 : 0);
    const uR = u('r'), uT = u('t');

    const SCALE = 1 / 6;
    function resize() {
      const w = Math.max(32, Math.ceil(innerWidth * SCALE)), h = Math.max(32, Math.ceil(innerHeight * SCALE));
      if (canvas.width === w && canvas.height === h) return;
      canvas.width = w;
      canvas.height = h;
      gl.viewport(0, 0, w, h);
      gl.uniform2f(uR, w, h);
    }
    const start = performance.now() - Math.random() * 60000; // chaque visite commence ailleurs
    const draw = (now) => { gl.uniform1f(uT, (now - start) / 1000); gl.drawArrays(gl.TRIANGLES, 0, 3); };
    resize();
    addEventListener('resize', () => { resize(); draw(performance.now()); });
    draw(performance.now());
    document.body.classList.add('fx-ready');

    if (!matchMedia('(prefers-reduced-motion: reduce)').matches) {
      let last = 0, raf = 0;
      const loop = (now) => {
        raf = requestAnimationFrame(loop);
        if (now - last < 50) return; // 20 images par seconde : le mouvement est lent, ça suffit
        last = now;
        draw(now);
      };
      raf = requestAnimationFrame(loop);
      document.addEventListener('visibilitychange', () => {
        cancelAnimationFrame(raf);
        if (!document.hidden) raf = requestAnimationFrame(loop);
      });
    }
  }
}
