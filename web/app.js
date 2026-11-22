document.documentElement.className = 'js';

const CONTENT = /*@CONTENT@*/null;
const ASSETS = /*@ASSETS@*/null;
const BUILD = /*@BUILD@*/null;

/* ------------------------------------------------------------------ */
/* helpers                                                             */
/* ------------------------------------------------------------------ */

const TAU = Math.PI * 2;
function clamp01(x) { return x < 0 ? 0 : x > 1 ? 1 : x; }
function easeOutCubic(x) { x = clamp01(x); const k = 1 - x; return 1 - k * k * k; }
function mulberry32(a) {
  return function () {
    a |= 0; a = (a + 0x6D2B79F5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
function h32(a, b, c) {
  let h = Math.imul(a | 0, 0x27d4eb2d) ^ Math.imul((b | 0) + 0x632be5ab, 0x165667b1) ^ Math.imul((c | 0) + 0x5bd1e995, 0x2c1b3c6d);
  h = Math.imul(h ^ (h >>> 15), 0x85ebca6b);
  h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35);
  h ^= h >>> 16;
  return (h >>> 0) / 4294967296;
}
const RAMP = [318, 282, 228, 190, 150, 56, 38, 56, 150, 190, 228, 282];
function rampHue(u) {
  u -= Math.floor(u);
  const s = u * 12, i = s | 0, f = s - i;
  return RAMP[i] + (RAMP[(i + 1) % 12] - RAMP[i]) * f;
}
const RGB = new Float32Array(3);
function hslRGB(h, s, l) {
  h -= 360 * Math.floor(h / 360);
  s = s < 0 ? 0 : s > 1 ? 1 : s;
  l = l < 0 ? 0 : l > 1 ? 1 : l;
  const c = (1 - Math.abs(2 * l - 1)) * s, hp = h / 60, x = c * (1 - Math.abs((hp % 2) - 1)), m = l - c / 2;
  let r, g, b;
  if (hp < 1) { r = c; g = x; b = 0; } else if (hp < 2) { r = x; g = c; b = 0; } else if (hp < 3) { r = 0; g = c; b = x; }
  else if (hp < 4) { r = 0; g = x; b = c; } else if (hp < 5) { r = x; g = 0; b = c; } else { r = c; g = 0; b = x; }
  RGB[0] = (r + m) * 255; RGB[1] = (g + m) * 255; RGB[2] = (b + m) * 255;
}
function rgbk(r, g, b, a) {
  let ai = (a * 15 + 0.5) | 0;
  if (ai <= 0) return -1;
  if (ai > 15) ai = 15;
  r = r < 0 ? 0 : r > 255 ? 255 : r; g = g < 0 ? 0 : g > 255 ? 255 : g; b = b < 0 ? 0 : b > 255 ? 255 : b;
  return (((r * 0.12157 + 0.5) | 0) << 14) | (((g * 0.12157 + 0.5) | 0) << 9) | (((b * 0.12157 + 0.5) | 0) << 4) | ai;
}
function hslk(h, s, l, a) { hslRGB(h, s * 0.01, l * 0.01); return rgbk(RGB[0], RGB[1], RGB[2], a === undefined ? 1 : a); }
const PAL = new Array(1 << 19);
function palStr(k) {
  let s = PAL[k];
  if (s === undefined) {
    const r = Math.round(((k >> 14) & 31) * 255 / 31), g = Math.round(((k >> 9) & 31) * 255 / 31), b = Math.round(((k >> 4) & 31) * 255 / 31);
    const a = (k & 15) / 15;
    s = a >= 1 ? 'rgb(' + r + ',' + g + ',' + b + ')' : 'rgba(' + r + ',' + g + ',' + b + ',' + a.toFixed(3) + ')';
    PAL[k] = s;
  }
  return s;
}
const CHS = [];
for (let i = 0; i < 128; i++) CHS.push(String.fromCharCode(i));
function code(s) { return s.charCodeAt(0); }
const C_AT = code('@'), C_HASH = code('#'), C_PLUS = code('+'), C_COLON = code(':'), C_DOT = code('.'), C_STAR = code('*');
const C_PIPE = code('|'), C_DASH = code('-'), C_SL = code('/'), C_BS = code('\\'), C_o = code('o'), C_PCT = code('%');
const SCRAMBLE = '#%&@*+=:<>/\\';

/* ------------------------------------------------------------------ */
/* canvas, grid, put, halos                                            */
/* ------------------------------------------------------------------ */

const cv = document.getElementById('c');
const mctx = cv.getContext('2d', { alpha: false });
const off = document.createElement('canvas'), octx = off.getContext('2d');
const bloomA = document.createElement('canvas'), actx = bloomA.getContext('2d');
const bloomB = document.createElement('canvas'), bctx = bloomB.getContext('2d');
const bgc = document.createElement('canvas'), bgctx = bgc.getContext('2d');
const FILTER_OK = typeof actx.filter === 'string';
const REDUCED = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);

let W = 1, H = 1, DPR = 1, PW = 1, CW = 8, CH = 13, AR = 1 / 1.62;
let cols = 0, rows = 0, gridX0 = 0, gridY0 = 0, stage0 = 0, stageCols = 124, cxs = 62;
let NCELL = 0, chr = null, clr = null, dep = null, sortBuf = null, XP = null, YP = null, FONT = '';
const NAVROWS = 3;

function layout() {
  W = Math.max(1, window.innerWidth);
  H = Math.max(1, window.innerHeight);
  DPR = Math.min(2, window.devicePixelRatio || 1);
  let sc, cw;
  if (W >= 124 * 7.4) { sc = 124; cw = Math.min(9.6, W / 124); }
  else { sc = Math.max(40, Math.floor(W / 7.4)); cw = W / sc; }
  CW = cw; CH = CW * 1.62; AR = CW / CH;
  PW = sc * CW;
  const stageLeft = (W - PW) / 2;
  stage0 = Math.max(0, Math.ceil(stageLeft / CW));
  cols = stage0 * 2 + sc;
  gridX0 = stageLeft - stage0 * CW;
  rows = Math.max(16, Math.floor(H / CH));
  gridY0 = (H - rows * CH) / 2;
  stageCols = sc;
  cxs = stage0 + sc / 2;
  NCELL = cols * rows;
  chr = new Uint8Array(NCELL); clr = new Int32Array(NCELL); dep = new Float32Array(NCELL); sortBuf = new Float64Array(NCELL);
  cv.width = Math.max(1, Math.round(W * DPR)); cv.height = Math.max(1, Math.round(H * DPR));
  cv.style.width = W + 'px'; cv.style.height = H + 'px';
  off.width = cv.width; off.height = cv.height;
  bloomA.width = Math.max(1, cv.width >> 1); bloomA.height = Math.max(1, cv.height >> 1);
  bloomB.width = Math.max(1, cv.width >> 2); bloomB.height = Math.max(1, cv.height >> 2);
  bgc.width = cv.width; bgc.height = cv.height;
  const gr = bgctx.createLinearGradient(0, 0, 0, bgc.height);
  gr.addColorStop(0, '#02030a'); gr.addColorStop(0.5, '#060a1a'); gr.addColorStop(0.82, '#100c26'); gr.addColorStop(1, '#1c1035');
  bgctx.fillStyle = gr; bgctx.fillRect(0, 0, bgc.width, bgc.height);
  XP = new Float32Array(cols); YP = new Float32Array(rows);
  for (let i = 0; i < cols; i++) XP[i] = (gridX0 + (i + 0.5) * CW) * DPR;
  for (let j = 0; j < rows; j++) YP[j] = (gridY0 + (j + 0.5) * CH) * DPR + CH * DPR * 0.04;
  FONT = 'bold ' + (0.95 * CH * DPR).toFixed(2) + 'px Menlo, Consolas, "DejaVu Sans Mono", monospace';
}

function put(x, y, c, k, z) {
  if (k < 0) return;
  const ix = Math.floor(x), iy = Math.floor(y);
  if (ix < 0 || iy < 0 || ix >= cols || iy >= rows) return;
  const i = iy * cols + ix;
  if (z >= dep[i]) { dep[i] = z; chr[i] = c; clr[i] = k; }
}

const HMAX = 300;
const HX = new Float32Array(HMAX), HY = new Float32Array(HMAX), HRAD = new Float32Array(HMAX);
const HHU = new Float32Array(HMAX), HSA = new Float32Array(HMAX), HLI = new Float32Array(HMAX), HST = new Float32Array(HMAX);
let haloN = 0;
function addHalo(x, y, rad, h, s, l, st) {
  if (haloN >= HMAX) return;
  HX[haloN] = x; HY[haloN] = y; HRAD[haloN] = rad; HHU[haloN] = h; HSA[haloN] = s; HLI[haloN] = l; HST[haloN] = st;
  haloN++;
}
function applyHalos(hf) {
  for (let n = 0; n < haloN; n++) {
    const x = HX[n], y = HY[n], R = HRAD[n], st = HST[n], ry = R * AR;
    const x0 = Math.max(0, Math.floor(x - R)), x1 = Math.min(cols - 1, Math.floor(x + R));
    const y0 = Math.max(0, Math.floor(y - ry)), y1 = Math.min(rows - 1, Math.floor(y + ry));
    for (let iy = y0; iy <= y1; iy++) {
      const dy = (iy + 0.5 - y) / AR;
      for (let ix = x0; ix <= x1; ix++) {
        const i = iy * cols + ix;
        if (chr[i]) continue;
        const dx = ix + 0.5 - x;
        const d = Math.sqrt(dx * dx + dy * dy) / R;
        if (d >= 1) continue;
        const q = 1 - d;
        if (h32(ix * 31 + n * 7, iy, hf) >= st * q * Math.sqrt(q)) continue;
        const k = hslk(HHU[n], HSA[n], HLI[n] * (1 - 0.28 * d), 0.85 - 0.42 * d);
        if (k < 0) continue;
        chr[i] = d < 0.34 ? C_PLUS : d < 0.67 ? C_COLON : C_DOT; clr[i] = k; dep[i] = -1e8;
      }
    }
  }
}

function render() {
  let n = 0;
  const N = NCELL;
  for (let i = 0; i < N; i++) if (chr[i]) sortBuf[n++] = clr[i] * N + i;
  const sb = sortBuf.subarray(0, n);
  sb.sort();
  octx.setTransform(1, 0, 0, 1, 0, 0);
  octx.clearRect(0, 0, off.width, off.height);
  octx.font = FONT; octx.textAlign = 'center'; octx.textBaseline = 'middle';
  let last = -1;
  for (let j = 0; j < n; j++) {
    const v = sb[j], k = Math.floor(v / N), i = v - k * N;
    if (k !== last) { octx.fillStyle = palStr(k); last = k; }
    const iy = (i / cols) | 0;
    octx.fillText(CHS[chr[i]], XP[i - iy * cols], YP[iy]);
  }
  mctx.setTransform(1, 0, 0, 1, 0, 0);
  mctx.globalCompositeOperation = 'source-over'; mctx.globalAlpha = 1;
  mctx.drawImage(bgc, 0, 0);
  mctx.drawImage(off, 0, 0);
  actx.setTransform(1, 0, 0, 1, 0, 0); actx.clearRect(0, 0, bloomA.width, bloomA.height);
  if (FILTER_OK) actx.filter = 'blur(' + (0.006 * PW * DPR * 0.5).toFixed(2) + 'px)';
  actx.drawImage(off, 0, 0, bloomA.width, bloomA.height);
  if (FILTER_OK) actx.filter = 'none';
  bctx.setTransform(1, 0, 0, 1, 0, 0); bctx.clearRect(0, 0, bloomB.width, bloomB.height);
  if (FILTER_OK) bctx.filter = 'blur(' + (0.02 * PW * DPR * 0.25).toFixed(2) + 'px)';
  bctx.drawImage(off, 0, 0, bloomB.width, bloomB.height);
  if (FILTER_OK) bctx.filter = 'none';
  mctx.globalCompositeOperation = 'screen';
  mctx.globalAlpha = 0.4; mctx.drawImage(bloomA, 0, 0, cv.width, cv.height);
  mctx.globalAlpha = 0.12; mctx.drawImage(bloomB, 0, 0, cv.width, cv.height);
  mctx.globalAlpha = 1; mctx.globalCompositeOperation = 'source-over';
}

/* ------------------------------------------------------------------ */
/* document layout: everything becomes text runs on document rows      */
/* ------------------------------------------------------------------ */

const ST_BODY = 0, ST_DIM = 1, ST_GOLD = 2, ST_LINK = 3, ST_ROLE = 4, ST_ORG = 5, ST_DATE = 6, ST_TAG = 7;
const ST_RAIL = 8, ST_BORDER = 9, ST_BANNER = 10, ST_SHADOW = 11, ST_BULLET = 12, ST_TITLE = 13, ST_BRACKET = 14;
const STYLE = [
  [220, 20, 86], [225, 14, 62], [44, 66, 76], [190, 70, 70], [210, 20, 94], [40, 62, 66], [205, 26, 62], [0, 0, 0],
  [220, 16, 42], [220, 14, 48], [44, 66, 78], [220, 20, 30], [44, 66, 70], [44, 40, 88], [220, 12, 50]
];

let DOC = [], REG = [], SECT = [], docRows = 0, heroRows = 0, contentW = 80, c0 = 0;
let curSec = -1;
let HERO = null;

function emit(row, col, text, st, reg, hue) {
  if (!text) return;
  if (!DOC[row]) DOC[row] = [];
  DOC[row].push({ c: col, s: text, st: st, r: reg === undefined ? -1 : reg, sec: curSec, h: hue || 0 });
  if (row + 1 > docRows) docRows = row + 1;
}
function region(row, col, len, kind, data) {
  REG.push({ row: row, c0: col, c1: col + len - 1, kind: kind, data: data });
  return REG.length - 1;
}
function wrap(text, width) {
  const words = String(text).split(/\s+/).filter(Boolean);
  const out = [];
  let line = '';
  for (let i = 0; i < words.length; i++) {
    let w = words[i];
    while (w.length > width) {
      if (line) { out.push(line); line = ''; }
      out.push(w.slice(0, width)); w = w.slice(width);
    }
    if (!line) line = w;
    else if (line.length + 1 + w.length <= width) line += ' ' + w;
    else { out.push(line); line = w; }
  }
  if (line) out.push(line);
  return out;
}
function bannerWidth(b) { return b && b.length ? b[0].length : 0; }
function hostOf(url) { return String(url).replace(/^https?:\/\//, '').replace(/\/$/, ''); }

const SECTIONS = [
  { id: 'about', nav: 'about' }, { id: 'experience', nav: 'experience' }, { id: 'skills', nav: 'skills' },
  { id: 'certifications', nav: 'certs' }, { id: 'education', nav: 'education' }, { id: 'projects', nav: 'projects' },
  { id: 'contact', nav: 'contact' }
];

function heading(row, id, label) {
  const b = ASSETS.banners.headings[id];
  if (b && bannerWidth(b) <= contentW) {
    for (let r = 0; r < b.length; r++) emit(row + r, c0, b[r], ST_BANNER);
    row += b.length;
  } else {
    emit(row, c0, '== ' + label.toUpperCase() + ' ==', ST_GOLD);
    row += 1;
  }
  emit(row, c0, '-'.repeat(Math.min(contentW, 48)), ST_RAIL);
  return row + 2;
}
function leader(row, left, right, stL, stR) {
  if (left.length + right.length + 4 <= contentW) {
    emit(row, c0, left, stL);
    const dots = contentW - left.length - right.length - 2;
    emit(row, c0 + left.length + 1, '.'.repeat(dots), ST_RAIL);
    emit(row, c0 + contentW - right.length, right, stR);
    return row + 1;
  }
  const ls = wrap(left, contentW);
  for (let i = 0; i < ls.length; i++) emit(row++, c0, ls[i], stL);
  emit(row++, c0 + 2, right, stR);
  return row;
}
function paragraph(row, col, width, text, st) {
  const ls = wrap(text, width);
  for (let i = 0; i < ls.length; i++) emit(row++, col, ls[i], st);
  return row;
}

function buildDoc() {
  DOC = []; REG = []; SECT = []; docRows = 0;
  contentW = Math.min(stageCols - 4, 92);
  c0 = stage0 + Math.floor((stageCols - contentW) / 2);
  heroRows = Math.max(rows, 30);
  curSec = -1;

  /* hero */
  const hb = ASSETS.banners;
  let nameLines;
  if (bannerWidth(hb.name) <= stageCols - 4) nameLines = [hb.name];
  else nameLines = hb.nameWords;
  const bannerH = nameLines.length * 8 - 1;
  const globeR = Math.max(8, Math.min(stageCols * 0.27, (heroRows * 0.24) / AR));
  const textBlock = bannerH + 7;
  const bottomFree = heroRows - textBlock - 3;
  HERO = { globeRow: Math.round(Math.max(NAVROWS + 2 + globeR * AR, Math.min(heroRows * 0.33, bottomFree - globeR * AR - 2))), globeR: globeR, banner: [], title: null };
  let r = Math.round(Math.min(heroRows - textBlock - 3, HERO.globeRow + globeR * AR + 3));
  for (let i = 0; i < nameLines.length; i++) {
    const b = nameLines[i];
    const col = Math.round(cxs - bannerWidth(b) / 2);
    HERO.banner.push({ row: r, col: col, rows: b });
    r += 8;
  }
  r += 1;
  const title = (CONTENT.title + '  //  ' + CONTENT.location).toUpperCase();
  const titleLine = title.length <= contentW ? title : CONTENT.title.toUpperCase();
  HERO.title = { row: r, col: Math.round(cxs - titleLine.length / 2), s: titleLine };
  r += 1;
  const tl = wrap(CONTENT.tagline, contentW);
  HERO.tagline = [];
  for (let i = 0; i < tl.length; i++) HERO.tagline.push({ row: r + i, col: Math.round(cxs - tl[i].length / 2), s: tl[i] });
  r += tl.length + 1;
  const prompt = '> scroll, use j / k, or pick a section';
  HERO.prompt = { row: r, col: Math.round(cxs - prompt.length / 2), s: prompt };
  HERO.hint = { row: heroRows - 2, col: Math.round(cxs) };

  let row = heroRows + 2;
  for (let si = 0; si < SECTIONS.length; si++) {
    const S = SECTIONS[si];
    curSec = si;
    SECT.push({ id: S.id, nav: S.nav, row: row, end: row, revealT: -1 });
    row = heading(row, S.id, S.id);
    if (S.id === 'about') {
      for (let i = 0; i < CONTENT.summary.length; i++) { row = paragraph(row, c0, contentW, CONTENT.summary[i], ST_BODY); row++; }
      row = leader(row, 'location', CONTENT.location, ST_DIM, ST_BODY);
      row = leader(row, 'focus', 'DevSecOps / platform / observability', ST_DIM, ST_BODY);
    } else if (S.id === 'experience') {
      const rail = c0 + 1;
      const firstRow = row;
      for (let j = 0; j < CONTENT.experience.length; j++) {
        const E = CONTENT.experience[j];
        const x = c0 + 4, w = contentW - 4;
        emit(row, rail, 'o', ST_GOLD);
        if (E.role.length + E.dates.length + 3 <= w) {
          emit(row, x, E.role, ST_ROLE);
          emit(row, x + w - E.dates.length, E.dates, ST_DATE);
          row++;
        } else { emit(row++, x, E.role, ST_ROLE); emit(row++, x, E.dates, ST_DATE); }
        row = paragraph(row, x, w, E.org + '  -  ' + E.place, ST_ORG);
        row++;
        for (let p = 0; p < E.points.length; p++) {
          const ls = wrap(E.points[p], w - 2);
          for (let q = 0; q < ls.length; q++) {
            if (q === 0) emit(row, x, '-', ST_BULLET);
            emit(row++, x + 2, ls[q], ST_BODY);
          }
        }
        row += 2;
      }
      for (let rr = firstRow; rr < row - 2; rr++) {
        const opsRow = DOC[rr];
        if (opsRow && opsRow.some(function (o) { return o.c === rail; })) continue;
        emit(rr, rail, '|', ST_RAIL);
      }
    } else if (S.id === 'skills') {
      for (let g = 0; g < CONTENT.skills.length; g++) {
        const G = CONTENT.skills[g];
        const hue = rampHue(g / CONTENT.skills.length);
        emit(row++, c0, G.group.toLowerCase(), ST_GOLD);
        let x = c0 + 2;
        for (let k = 0; k < G.items.length; k++) {
          const tag = '[' + G.items[k] + ']';
          if (x + tag.length > c0 + contentW) { row++; x = c0 + 2; }
          emit(row, x, tag, ST_TAG, -1, hue);
          x += tag.length + 1;
        }
        row += 2;
      }
    } else if (S.id === 'certifications') {
      for (let k = 0; k < CONTENT.certifications.length; k++) {
        const C = CONTENT.certifications[k];
        row = leader(row, '[*] ' + C.name, C.dates, ST_BODY, ST_DATE);
        row++;
      }
    } else if (S.id === 'education') {
      for (let k = 0; k < CONTENT.education.length; k++) {
        const E = CONTENT.education[k];
        row = leader(row, E.degree, E.dates, ST_ROLE, ST_DATE);
        row = paragraph(row, c0 + 2, contentW - 2, E.school, ST_ORG);
        row++;
      }
    } else if (S.id === 'projects') {
      for (let k = 0; k < CONTENT.projects.length; k++) {
        const P = CONTENT.projects[k];
        const w = contentW;
        const top = '+-- ' + P.name + ' ';
        emit(row, c0, top + '-'.repeat(Math.max(0, w - top.length - 1)) + '+', ST_BORDER);
        emit(row, c0 + 4, P.name, ST_ROLE);
        row++;
        const inner = w - 4;
        const lines = [{ s: P.stack, st: ST_ORG }, { s: '', st: ST_BODY }];
        const tx = wrap(P.text, inner);
        for (let q = 0; q < tx.length; q++) lines.push({ s: tx[q], st: ST_BODY });
        for (let q = 0; q < lines.length; q++) {
          emit(row, c0, '|', ST_BORDER); emit(row, c0 + w - 1, '|', ST_BORDER);
          emit(row, c0 + 2, lines[q].s, lines[q].st);
          row++;
        }
        if (P.href) {
          emit(row, c0, '|', ST_BORDER); emit(row, c0 + w - 1, '|', ST_BORDER);
          const lab = '[ open -> ]';
          const reg = region(row, c0 + 2, lab.length, 'link', { href: P.href, label: P.name, external: false });
          emit(row, c0 + 2, lab, ST_LINK, reg);
          row++;
        }
        emit(row, c0, '+' + '-'.repeat(w - 2) + '+', ST_BORDER);
        row += 2;
      }
    } else if (S.id === 'contact') {
      row = paragraph(row, c0, contentW, 'Open to conversations about DevSecOps, platform engineering, and secure delivery.', ST_BODY);
      row++;
      for (let k = 0; k < CONTENT.links.length; k++) {
        const L = CONTENT.links[k];
        const lab = (L.label + '          ').slice(0, 10);
        const txt = '> ' + lab + hostOf(L.url);
        const reg = region(row, c0, txt.length, 'link', { href: L.url, label: L.label, external: true });
        emit(row, c0, txt, ST_LINK, reg);
        row += 2;
      }
      if (CONTENT.email) {
        const txt = '> email     ' + CONTENT.email;
        const reg = region(row, c0, txt.length, 'link', { href: 'mailto:' + CONTENT.email, label: 'Email', external: true });
        emit(row, c0, txt, ST_LINK, reg);
        row += 2;
      }
      const pt = '[ plain-text version ]';
      const reg = region(row, c0, pt.length, 'plain', null);
      emit(row, c0, pt, ST_BRACKET, reg);
      row += 4;
      curSec = -1;
      const stamp = BUILD ? 'build ' + BUILD.date + '  ' + BUILD.id : 'dev build';
      emit(row++, c0, 'rendered entirely in text  -  C++ / Python / Go', ST_DIM);
      emit(row++, c0, stamp, ST_RAIL);
      row += 2;
    }
    SECT[si].end = row;
    row += 3;
  }
  docRows = Math.max(docRows, row);
  buildRegIndex();
}
let REGROW = [];
function buildRegIndex() {
  REGROW = [];
  for (let i = 0; i < REG.length; i++) {
    const r = REG[i].row;
    if (!REGROW[r]) REGROW[r] = [];
    REGROW[r].push(i);
  }
}

/* ------------------------------------------------------------------ */
/* scrolling, navigation and input                                     */
/* ------------------------------------------------------------------ */

let scrollPos = 0, scrollTarget = 0, scrollRow = 0;
function maxScroll() { return Math.max(0, docRows - rows + 1); }
function clampTarget() { scrollTarget = Math.max(0, Math.min(maxScroll(), scrollTarget)); }
function scrollToSection(id) {
  for (let i = 0; i < SECT.length; i++) {
    if (SECT[i].id === id) {
      scrollTarget = SECT[i].row - NAVROWS - 1;
      clampTarget();
      if (REDUCED) scrollPos = scrollTarget;
      if (history.replaceState) history.replaceState(null, '', '#' + id);
      return true;
    }
  }
  return false;
}
function activeSection() {
  const probe = scrollRow + NAVROWS + Math.floor(rows * 0.35);
  let a = -1;
  for (let i = 0; i < SECT.length; i++) if (SECT[i].row <= probe) a = i;
  return a;
}

let NAVREG = [];
let menuOpen = false;
let hover = null;
let soundOn = false;

function navLayout() {
  NAVREG = [];
  const left = 'jdj@ascii:~$';
  const items = [];
  let total = left.length + 2;
  for (let i = 0; i < SECTIONS.length; i++) total += SECTIONS[i].nav.length + 3;
  const snd = soundOn ? '[sound on]' : '[sound off]';
  total += snd.length;
  const wide = total <= stageCols - 2;
  let x = stage0 + 1;
  NAVREG.push({ row: 1, c0: x, c1: x + left.length - 1, kind: 'top', s: left });
  if (wide) {
    let xr = stage0 + stageCols - 1 - snd.length;
    NAVREG.push({ row: 1, c0: xr, c1: xr + snd.length - 1, kind: 'sound', s: snd });
    xr -= 2;
    for (let i = SECTIONS.length - 1; i >= 0; i--) {
      const s = SECTIONS[i].nav;
      xr -= s.length + 2;
      items.push({ row: 1, c0: xr, c1: xr + s.length + 1, kind: 'nav', data: SECTIONS[i].id, s: s, idx: i });
      xr -= 1;
    }
    for (let i = 0; i < items.length; i++) NAVREG.push(items[i]);
  } else {
    let xr = stage0 + stageCols - 1 - snd.length;
    NAVREG.push({ row: 1, c0: xr, c1: xr + snd.length - 1, kind: 'sound', s: snd });
    const m = menuOpen ? '[close]' : '[menu]';
    xr -= m.length + 2;
    NAVREG.push({ row: 1, c0: xr, c1: xr + m.length - 1, kind: 'menu', s: m });
    if (menuOpen) {
      for (let i = 0; i < SECTIONS.length; i++) {
        const s = '> ' + SECTIONS[i].nav;
        NAVREG.push({ row: 3 + i * 2, c0: stage0 + stageCols - 16, c1: stage0 + stageCols - 16 + s.length - 1, kind: 'nav', data: SECTIONS[i].id, s: s, idx: i, menu: true });
      }
    }
  }
}

function cellAt(ev) {
  return { col: Math.floor((ev.clientX - gridX0) / CW), row: Math.floor((ev.clientY - gridY0) / CH) };
}
function hitTest(col, row) {
  for (let i = 0; i < NAVREG.length; i++) {
    const R = NAVREG[i];
    if (R.kind !== 'top' && R.row === row && col >= R.c0 && col <= R.c1) return { nav: R };
  }
  if (row < NAVROWS || menuOpen) return null;
  const dr = row + scrollRow;
  const list = REGROW[dr];
  if (!list) return null;
  for (let i = 0; i < list.length; i++) {
    const R = REG[list[i]];
    if (col >= R.c0 && col <= R.c1) return { doc: list[i] };
  }
  return null;
}
function sameHover(a, b) {
  if (!a || !b) return a === b;
  return a.nav === b.nav && a.doc === b.doc;
}
function activate(hit) {
  if (!hit) return;
  if (hit.nav) {
    const R = hit.nav;
    if (R.kind === 'nav') { scrollToSection(R.data); menuOpen = false; sfx('click', R.idx); }
    else if (R.kind === 'menu') { menuOpen = !menuOpen; sfx('click', 0); }
    else if (R.kind === 'sound') toggleSound();
    return;
  }
  const R = REG[hit.doc];
  if (R.kind === 'link') {
    sfx('click', 3);
    if (R.data.external) {
      const w = window.open(R.data.href, '_blank', 'noopener,noreferrer');
      if (w) w.opener = null;
    } else window.location.href = R.data.href;
  } else if (R.kind === 'plain') {
    showPlain(true);
  }
}
function showPlain(on) {
  document.documentElement.className = on ? 'plain' : 'js';
  if (!on) { layout(); buildDoc(); }
}

cv.addEventListener('mousemove', function (ev) {
  const c = cellAt(ev);
  const h = hitTest(c.col, c.row);
  if (!sameHover(h, hover)) { if (h) sfx('hover', c.col + c.row); hover = h; }
  cv.style.cursor = h ? 'pointer' : 'default';
});
cv.addEventListener('mouseleave', function () { hover = null; });
cv.addEventListener('click', function (ev) {
  const c = cellAt(ev);
  activate(hitTest(c.col, c.row));
});
window.addEventListener('wheel', function (ev) {
  if (document.documentElement.className !== 'js') return;
  ev.preventDefault();
  const unit = ev.deltaMode === 1 ? 3 : ev.deltaMode === 2 ? rows : 1 / CH;
  scrollTarget += ev.deltaY * unit;
  clampTarget();
}, { passive: false });
let touchY = null, touchStart = 0;
cv.addEventListener('touchstart', function (ev) { touchY = ev.touches[0].clientY; touchStart = scrollTarget; }, { passive: true });
cv.addEventListener('touchmove', function (ev) {
  if (touchY === null) return;
  ev.preventDefault();
  scrollTarget = touchStart + (touchY - ev.touches[0].clientY) / CH;
  clampTarget();
  scrollPos = scrollTarget;
}, { passive: false });
cv.addEventListener('touchend', function () { touchY = null; }, { passive: true });
window.addEventListener('keydown', function (ev) {
  if (document.documentElement.className !== 'js') return;
  if (ev.target && ev.target.tagName === 'A') return;
  const page = Math.max(4, rows - NAVROWS - 3);
  const k = ev.key;
  let used = true;
  if (k === 'ArrowDown' || k === 'j') scrollTarget += 3;
  else if (k === 'ArrowUp' || k === 'k') scrollTarget -= 3;
  else if (k === 'PageDown' || (k === ' ' && !ev.shiftKey)) scrollTarget += page;
  else if (k === 'PageUp' || (k === ' ' && ev.shiftKey)) scrollTarget -= page;
  else if (k === 'Home' || k === 'g') scrollTarget = 0;
  else if (k === 'End' || k === 'G') scrollTarget = maxScroll();
  else if (k === 'Escape') menuOpen = false;
  else used = false;
  if (used) { ev.preventDefault(); clampTarget(); if (REDUCED) scrollPos = scrollTarget; }
});
window.addEventListener('resize', function () {
  const a = activeSection();
  layout(); buildDoc();
  if (a >= 0) { scrollTarget = SECT[a].row - NAVROWS - 1; clampTarget(); scrollPos = scrollTarget; }
});
document.addEventListener('focusin', function (ev) {
  const el = ev.target;
  if (!el || !el.getAttribute) return;
  const sec = el.getAttribute('data-section');
  if (sec) scrollToSection(sec);
});
const backBtn = document.getElementById('back-ascii');
if (backBtn) backBtn.addEventListener('click', function () { showPlain(false); });

/* ------------------------------------------------------------------ */
/* sound: synthesized, off until the visitor turns it on               */
/* ------------------------------------------------------------------ */

let AC = null, BUS = null, NOISE = null, droneNodes = null;
const DEG = [0, 2, 4, 7, 9];
function NOTE(i) {
  const o = Math.floor(i / 5), d = DEG[((i % 5) + 5) % 5];
  return 523.25 * Math.pow(2, (d + 12 * o) / 12);
}
function initAudio() {
  try {
    if (!AC) {
      const Ctor = window.AudioContext || window.webkitAudioContext;
      if (!Ctor) return;
      AC = new Ctor();
      const sr = AC.sampleRate;
      NOISE = AC.createBuffer(1, Math.floor(sr * 2), sr);
      const nd = NOISE.getChannelData(0);
      for (let i = 0; i < nd.length; i++) nd[i] = Math.random() * 2 - 1;
      const len = Math.floor(sr * 2.4), ir = AC.createBuffer(2, len, sr);
      for (let ch = 0; ch < 2; ch++) {
        const d = ir.getChannelData(ch);
        for (let i = 0; i < len; i++) { const q = 1 - i / len; d[i] = (Math.random() * 2 - 1) * q * q * q; }
      }
      BUS = AC.createGain();
      const dry = AC.createGain(); dry.gain.value = 0.85;
      const conv = AC.createConvolver(); conv.buffer = ir;
      const wet = AC.createGain(); wet.gain.value = 0.32;
      const comp = AC.createDynamicsCompressor(); comp.threshold.value = -16; comp.ratio.value = 4;
      const master = AC.createGain(); master.gain.value = 1.3;
      BUS.connect(dry); dry.connect(comp); BUS.connect(conv); conv.connect(wet); wet.connect(comp);
      comp.connect(master); master.connect(AC.destination);
    }
    if (AC.state !== 'running') AC.resume();
  } catch (e) { AC = null; }
}
function audioOK() { return soundOn && !!AC && AC.state === 'running'; }
function outNode(pan) {
  if (!pan || !AC.createStereoPanner) return BUS;
  const p = AC.createStereoPanner(); p.pan.value = Math.max(-1, Math.min(1, pan)); p.connect(BUS);
  return p;
}
function bell(freq, at, vol, decay, pan) {
  const out = outNode(pan), parts = [1, 2.76, 5.4], lv = [1, 0.2, 0.07];
  for (let i = 0; i < 3; i++) {
    const f = freq * parts[i];
    if (f > 18000) continue;
    const o = AC.createOscillator(); o.type = 'sine'; o.frequency.setValueAtTime(f, at);
    const g = AC.createGain(), dk = decay / (1 + i * 0.9);
    g.gain.setValueAtTime(0, at); g.gain.linearRampToValueAtTime(vol * lv[i], at + 0.004);
    g.gain.exponentialRampToValueAtTime(0.0001, at + 0.004 + dk);
    o.connect(g); g.connect(out); o.start(at); o.stop(at + 0.004 + dk + 0.05);
  }
}
function noiseBurst(at, dur, f0, f1, q, vol, attack, pan) {
  const src = AC.createBufferSource(); src.buffer = NOISE; src.loop = true;
  const bq = AC.createBiquadFilter(); bq.type = 'bandpass'; bq.Q.setValueAtTime(q, at);
  bq.frequency.setValueAtTime(f0, at); bq.frequency.exponentialRampToValueAtTime(f1, at + dur);
  const g = AC.createGain(); g.gain.setValueAtTime(0, at); g.gain.linearRampToValueAtTime(vol, at + attack);
  g.gain.exponentialRampToValueAtTime(0.0001, at + dur);
  src.connect(bq); bq.connect(g); g.connect(outNode(pan)); src.start(at, Math.random() * 1.5); src.stop(at + dur + 0.05);
}
function startDrone() {
  if (!AC || droneNodes) return;
  const n = AC.currentTime + 0.02;
  const g = AC.createGain();
  g.gain.setValueAtTime(0, n); g.gain.linearRampToValueAtTime(0.018, n + 2.5);
  g.connect(BUS);
  const freqs = [NOTE(-10), NOTE(-8), NOTE(-6)];
  const oscs = [];
  for (let i = 0; i < freqs.length; i++) {
    const o = AC.createOscillator(); o.type = 'sine';
    o.frequency.setValueAtTime(freqs[i] * (1 + (i - 1) * 0.0015), n);
    o.connect(g); o.start(n); oscs.push(o);
  }
  droneNodes = { g: g, oscs: oscs };
}
function stopDrone() {
  if (!AC || !droneNodes) return;
  const n = AC.currentTime;
  droneNodes.g.gain.cancelScheduledValues(n);
  droneNodes.g.gain.setValueAtTime(Math.max(0.0001, droneNodes.g.gain.value), n);
  droneNodes.g.gain.exponentialRampToValueAtTime(0.0001, n + 0.6);
  for (let i = 0; i < droneNodes.oscs.length; i++) droneNodes.oscs[i].stop(n + 0.65);
  droneNodes = null;
}
function toggleSound() {
  soundOn = !soundOn;
  if (soundOn) {
    initAudio();
    startDrone();
    if (AC) { const n = AC.currentTime + 0.02; bell(NOTE(0), n, 0.06, 1.4, -0.3); bell(NOTE(4), n + 0.08, 0.05, 1.4, 0.3); }
  } else stopDrone();
  navLayout();
}
let lastHoverSfx = 0;
function sfx(kind, n) {
  if (!audioOK()) return;
  const t = AC.currentTime + 0.005;
  if (kind === 'hover') {
    if (t - lastHoverSfx < 0.06) return;
    lastHoverSfx = t;
    bell(NOTE(10 + (n % 5)), t, 0.018, 0.5, 0);
  } else if (kind === 'click') {
    bell(NOTE(5 + (n % 5)), t, 0.06, 1.2, -0.2);
    bell(NOTE(7 + (n % 5)), t + 0.05, 0.05, 1.2, 0.2);
    noiseBurst(t, 0.12, 2400, 4200, 2, 0.02, 0.01, 0);
  } else if (kind === 'section') {
    bell(NOTE(n), t, 0.035, 1.6, (n % 3 - 1) * 0.5);
  }
}

/* ------------------------------------------------------------------ */
/* drawing                                                             */
/* ------------------------------------------------------------------ */

const rng = mulberry32(31337);

/* starfield: three parallax layers of twinkling stars, plus shooting stars */
const STARN = 420;
const SU = new Float32Array(STARN), SY = new Float32Array(STARN), SD = new Float32Array(STARN);
const SB = new Float32Array(STARN), SF = new Float32Array(STARN), SPH = new Float32Array(STARN), SHUE = new Float32Array(STARN), SSAT = new Float32Array(STARN);
for (let i = 0; i < STARN; i++) {
  SU[i] = -1.5 + 4 * rng(); SY[i] = rng();
  const d = rng(); SD[i] = d * d;
  SB[i] = 0.25 + 0.75 * Math.pow(rng(), 2.2);
  SF[i] = 0.6 + 2.4 * rng(); SPH[i] = rng() * TAU;
  const c = rng();
  SHUE[i] = c < 0.55 ? 215 : c < 0.8 ? 45 : c < 0.92 ? 270 : 10;
  SSAT[i] = c < 0.55 ? 45 : 60;
}
let starHalos = 0;
function drawStars(t) {
  const span = rows + 2;
  starHalos = 0;
  for (let i = 0; i < STARN; i++) {
    const x = stage0 + SU[i] * stageCols;
    if (x < 0 || x >= cols) continue;
    let y = SY[i] - scrollPos * (0.02 + 0.25 * SD[i]) / span;
    y -= Math.floor(y);
    const tw = REDUCED ? 1 : 0.55 + 0.45 * Math.sin(t * SF[i] + SPH[i]);
    const b = SB[i] * tw;
    const ch = b > 0.82 ? C_STAR : b > 0.5 ? C_PLUS : C_DOT;
    const k = hslk(SHUE[i], SSAT[i], 62 + 34 * b, 0.25 + 0.75 * b);
    put(x, y * span - 1, ch, k, -1e7 + b);
    if (b > 0.9 && starHalos < 12) { addHalo(x, y * span - 1, 1.8, SHUE[i], 40, 85, 0.35); starHalos++; }
  }
  if (REDUCED) return;
  for (let s = 0; s < 2; s++) {
    const period = 6.5 + s * 2.3;
    const cyc = Math.floor((t + s * 3.1) / period);
    const u = ((t + s * 3.1) - cyc * period) / 0.9;
    if (u < 0 || u > 1) continue;
    const x0 = stage0 + (0.1 + 0.8 * h32(cyc, s, 1)) * stageCols, y0 = rows * (0.05 + 0.35 * h32(cyc, s, 2));
    const dir = h32(cyc, s, 3) < 0.5 ? -1 : 1;
    const len = 18 + 14 * h32(cyc, s, 4);
    const hx = x0 + dir * len * u, hy = y0 + len * u * 0.45 * AR * 1.6;
    for (let k = 0; k < 14; k++) {
      const f = k / 14;
      const tx = hx - dir * f * 9, ty = hy - f * 9 * 0.45 * AR * 1.6;
      put(tx, ty, k === 0 ? C_STAR : k < 5 ? C_DASH : C_DOT, hslk(200, 40, 95 - 30 * f, (1 - f) * Math.sin(Math.PI * u)), -1e6);
    }
    addHalo(hx, hy, 2.2, 200, 40, 90, 0.5 * Math.sin(Math.PI * u));
  }
}

/* supernova remnant built from the C++ scene: knots = nodes, filaments = edges, nebula = shell */
const SH = ASSETS.constellation.shell, ND = ASSETS.constellation.nodes, ED = ASSETS.constellation.edges;
const KNOT_HUE = [340, 178, 28];
const PROJ = new Float32Array(ND.length * 3);
const SHN = new Float32Array(SH.length * 4);
for (let i = 0; i < SH.length; i++) {
  const p = SH[i];
  const n = 0.78 + 0.42 * h32(Math.round(p[0] * 7), Math.round(p[1] * 7), Math.round(p[2] * 7)) + 0.08 * (h32(i, 5, 5) - 0.5);
  SHN[i * 4] = p[0] * n + 0.06 * (h32(i, 1, 9) - 0.5);
  SHN[i * 4 + 1] = p[1] * n + 0.06 * (h32(i, 2, 9) - 0.5);
  SHN[i * 4 + 2] = p[2] * n + 0.06 * (h32(i, 3, 9) - 0.5);
  SHN[i * 4 + 3] = h32(i, 4, 9);
}
function projPoint(x, y, z, rot, tilt, R, gx, gy, out, o) {
  const cr = Math.cos(rot), sr = Math.sin(rot), ct = Math.cos(tilt), st = Math.sin(tilt);
  const x1 = x * cr + z * sr, z1 = -x * sr + z * cr;
  const y2 = y * ct - z1 * st, z2 = y * st + z1 * ct;
  const s = 1 + 0.16 * z2;
  out[o] = gx + x1 * R * s; out[o + 1] = gy - y2 * R * s * AR; out[o + 2] = z2;
}
const PTMP = new Float32Array(3);
function ring3(gx, gy, R, rot, tilt, radius, ch, k, z) {
  const n = Math.max(24, Math.ceil(radius * R * 6));
  for (let i = 0; i < n; i++) {
    const a = i / n * TAU;
    projPoint(Math.cos(a) * radius, 0, Math.sin(a) * radius, rot, tilt, R, gx, gy, PTMP, 0);
    put(PTMP[0], PTMP[1], ch, k, PTMP[2] * 10 + z);
  }
}
function drawSupernova(t, introT) {
  const gx = cxs, gy = HERO.globeRow - scrollRow, R = HERO.globeR;
  if (gy + R * AR * 1.5 < 0) return;
  const it = REDUCED ? 99 : introT;
  if (it < 0.35) return;
  const BOOM = 0.95;
  if (it < BOOM) {
    const u = (it - 0.35) / (BOOM - 0.35);
    const swell = u < 0.7 ? 1 + 1.6 * (u / 0.7) : 2.6 * (1 - (u - 0.7) / 0.3) + 0.2;
    const rr = swell * 1.4;
    for (let dy = -Math.ceil(rr * AR); dy <= Math.ceil(rr * AR); dy++) {
      for (let dx = -Math.ceil(rr); dx <= Math.ceil(rr); dx++) {
        const d = Math.sqrt(dx * dx + (dy / AR) * (dy / AR));
        if (d > rr) continue;
        put(gx + dx, gy + dy, d < rr * 0.5 ? C_AT : C_HASH, hslk(u < 0.7 ? 20 + 25 * u : 210, 80, 60 + 30 * u), 40);
      }
    }
    addHalo(gx, gy, 3 + 5 * swell, u < 0.7 ? 25 : 210, 70, 85, 0.6);
    return;
  }
  const since = it - BOOM;
  const grow = REDUCED ? 1 : easeOutCubic(since / 1.1);
  const rot = t * (REDUCED ? 0.02 : 0.08), tilt = 0.42;
  if (since < 0.9) {
    const f = 1 - since / 0.9;
    addHalo(gx, gy, 6 + 22 * (1 - f), 45, 30, 98, 0.9 * f);
    const sr = (0.15 + 1.5 * easeOutCubic(since / 0.9));
    ring3(gx, gy, R, rot, tilt, sr, C_HASH, hslk(200, 50, 96, f), 30);
  }
  for (let i = 0; i < SH.length; i++) {
    if (SHN[i * 4 + 3] > 0.55) continue;
    const breathe = grow * (1 + 0.03 * Math.sin(t * 0.7 + SHN[i * 4 + 3] * 6));
    projPoint(SHN[i * 4], SHN[i * 4 + 1], SHN[i * 4 + 2], rot, tilt, R * breathe, gx, gy, PTMP, 0);
    const d = PTMP[2];
    const ang = Math.atan2(SHN[i * 4 + 1], SHN[i * 4]) / TAU + 0.5;
    const hue = 280 + 80 * Math.sin(ang * TAU + t * 0.1);
    const front = d > -0.2;
    put(PTMP[0], PTMP[1], SHN[i * 4 + 3] < 0.12 ? C_COLON : C_DOT, hslk(hue, 70, front ? 58 + 12 * d : 36, front ? 0.62 : 0.3), d * 10 - 20);
  }
  for (let i = 0; i < ND.length; i++) {
    const n = ND[i];
    const fly = REDUCED ? 1 : easeOutCubic((since - h32(i, 7, 1) * 0.25) / 1.0);
    const k = Math.max(0, fly) * (1 + 0.04 * Math.sin(t * 0.9 + i));
    projPoint(n[0] * k, n[1] * k, n[2] * k, rot, tilt, R, gx, gy, PROJ, i * 3);
  }
  const edgeA = REDUCED ? 1 : clamp01((since - 0.6) / 0.8);
  if (edgeA > 0) {
    for (let e = 0; e < ED.length; e++) {
      const a = ED[e][0], b = ED[e][1];
      const ax = PROJ[a * 3], ay = PROJ[a * 3 + 1], az = PROJ[a * 3 + 2];
      const bx = PROJ[b * 3], by = PROJ[b * 3 + 1], bz = PROJ[b * 3 + 2];
      const dx = bx - ax, dy = (by - ay) / AR;
      const len = Math.sqrt(dx * dx + dy * dy);
      const n = Math.max(2, Math.ceil(len / 0.8));
      const adx = Math.abs(dx), ady = Math.abs(dy);
      const c = adx < 0.4 * ady ? C_PIPE : ady < 0.4 * adx ? C_DASH : ((dx > 0) === (dy > 0) ? C_BS : C_SL);
      const hue = KNOT_HUE[ND[a][3]];
      for (let s = 1; s < n; s++) {
        const f = s / n, z = az + (bz - az) * f;
        if (h32(e, s, 3) < 0.3) continue;
        const front = z > -0.1;
        put(ax + dx * f, ay + (by - ay) * f, front && h32(e, s, 4) < 0.6 ? c : C_DOT,
          hslk(hue, front ? 65 : 40, front ? 48 + 12 * z : 30, (front ? 0.7 : 0.3) * edgeA), z * 10 - 5);
      }
    }
  }
  for (let i = 0; i < ND.length; i++) {
    const z = PROJ[i * 3 + 2], hue = KNOT_HUE[ND[i][3]];
    const front = z > 0;
    const tw = 0.75 + 0.25 * Math.sin(t * 2.1 + i * 1.7);
    put(PROJ[i * 3], PROJ[i * 3 + 1], front ? (i % 7 === 0 ? C_STAR : C_AT) : C_o, hslk(hue, 75, (front ? 58 + 16 * z : 38) * tw), z * 10);
  }
  if (!REDUCED) {
    for (let r = 0; r < 2; r++) {
      let u = (t * 0.25 + r * 0.5) % 1;
      const rad = 0.25 + 1.15 * u;
      ring3(gx, gy, R, rot * 0.5 + r, tilt, rad * grow, u < 0.5 ? C_DASH : C_DOT, hslk(195, 55, 88, 0.85 * (1 - u)), 2);
    }
    for (let k = 0; k < 40; k++) {
      const life = 2.6 + 1.4 * h32(k, 1, 1);
      const u = ((t + h32(k, 2, 2) * life) % life) / life;
      const cyc = Math.floor((t + h32(k, 2, 2) * life) / life);
      const th = h32(k, cyc, 3) * TAU, ph = Math.acos(2 * h32(k, cyc, 4) - 1);
      const rr = 0.1 + 1.35 * u;
      projPoint(Math.sin(ph) * Math.cos(th) * rr, Math.cos(ph) * rr, Math.sin(ph) * Math.sin(th) * rr, rot, tilt, R * grow, gx, gy, PTMP, 0);
      put(PTMP[0], PTMP[1], u < 0.3 ? C_STAR : u < 0.65 ? C_PLUS : C_DOT, hslk(35, 80, 85 - 30 * u, 1 - u), PTMP[2] * 10 + 1);
    }
    const ba = t * 1.3;
    for (let side = -1; side <= 1; side += 2) {
      for (let s = 2; s < 26; s++) {
        const f = s / 26;
        const bx = Math.cos(ba) * f * 1.25 * side, by = 0.92 * f * side, bz = Math.sin(ba) * f * 1.25 * side;
        projPoint(bx, by, bz, rot, tilt, R * grow, gx, gy, PTMP, 0);
        if (h32(s, side, Math.floor(t * 9)) < 0.25) continue;
        put(PTMP[0], PTMP[1], s < 8 ? C_PIPE : C_COLON, hslk(190, 70, 92 - 40 * f, 0.85 - 0.7 * f), PTMP[2] * 10 + 8);
      }
    }
  }
  const pulse = 0.75 + 0.25 * Math.sin(t * 5.2);
  const cr = (2.2 + 0.5 * pulse) * Math.max(0.3, grow);
  for (let dy = -Math.ceil(cr * AR); dy <= Math.ceil(cr * AR); dy++) {
    for (let dx = -Math.ceil(cr); dx <= Math.ceil(cr); dx++) {
      const d = Math.sqrt(dx * dx + (dy / AR) * (dy / AR)) / cr;
      if (d > 1) continue;
      const ch = d < 0.25 ? C_AT : d < 0.55 ? C_HASH : d < 0.8 ? C_STAR : C_PLUS;
      put(gx + dx, gy + dy, ch, hslk(48 - 20 * d, 40 + 50 * d, 98 - 26 * d * pulse), 60 - d);
    }
  }
  addHalo(gx, gy, cr + 5 + 2 * pulse, 40, 70, 90, 0.8);
}

function drawBanner(B, introT, revealStart, gold) {
  const top = B.row - scrollRow;
  if (top + B.rows.length < NAVROWS || top >= rows) return;
  const w = B.rows[0].length;
  for (let r = 0; r < B.rows.length; r++) {
    const line = B.rows[r], sy = top + r;
    for (let c = 0; c < w; c++) {
      if (line.charCodeAt(c) !== 35) continue;
      const x = B.col + c;
      const tReveal = revealStart + 0.55 * (c / w) + 0.12 * h32(x, sy + scrollRow, 4);
      if (!REDUCED && introT < tReveal) {
        if (introT > tReveal - 0.35) put(x + 0.5, sy + 0.5, code(SCRAMBLE[(h32(x, sy, Math.floor(introT * 18)) * SCRAMBLE.length) | 0]), hslk(44, 30, 50, 0.7), 20);
        continue;
      }
      if (sy >= NAVROWS || scrollRow === 0) {
        put(x + 1.5, sy + 1.5, C_DOT, hslk(260, 30, 28), 15);
        const L = (gold ? 84 : 80) - r * 2.5;
        const ch = h32(x, r, Math.floor(introT * 2)) < 0.06 ? C_AT : C_HASH;
        put(x + 0.5, sy + 0.5, ch, hslk(44, gold ? 66 : 50, L), 20);
      }
    }
  }
}
function drawTextAt(row, col, s, k, z, typeT, t) {
  const sy = row - scrollRow;
  if (sy < NAVROWS && scrollRow > 0) return;
  if (sy < 0 || sy >= rows) return;
  const n = REDUCED ? s.length : Math.min(s.length, Math.floor((t - typeT) * 60));
  for (let i = 0; i < n; i++) {
    const c = s.charCodeAt(i);
    if (c !== 32) put(col + i + 0.5, sy + 0.5, c, k, 20);
  }
  if (!REDUCED && n < s.length && n >= 0 && t > typeT) put(col + n + 0.5, sy + 0.5, C_HASH, hslk(195, 60, 75), 21);
}

function styleKey(st, hue, hot) {
  if (st === ST_TAG) return hslk(hue, 45, hot ? 82 : 66);
  const S = STYLE[st];
  if (hot) return hslk(S[0], S[1] + 10, Math.min(96, S[2] + 16));
  return hslk(S[0], S[1], S[2]);
}
function drawDoc(t) {
  const hoverReg = hover && hover.doc !== undefined ? hover.doc : -1;
  for (let sy = NAVROWS; sy < rows; sy++) {
    const dr = sy + scrollRow;
    const ops = DOC[dr];
    if (!ops) continue;
    for (let o = 0; o < ops.length; o++) {
      const op = ops[o];
      if (op.st === ST_BANNER) continue;
      const S = op.sec >= 0 ? SECT[op.sec] : null;
      const rt = S ? S.revealT : 0;
      const hot = op.r >= 0 && op.r === hoverReg;
      const k = styleKey(op.st, op.h, hot);
      const s = op.s;
      for (let i = 0; i < s.length; i++) {
        const c = s.charCodeAt(i);
        if (c === 32) continue;
        const x = op.c + i;
        if (S && !REDUCED) {
          const delay = Math.min(0.6, 0.004 * (x - c0) + 0.015 * (dr - S.row));
          if (rt < 0 || t < rt + delay) continue;
          if (t < rt + delay + 0.22) {
            put(x + 0.5, sy + 0.5, code(SCRAMBLE[(h32(x, dr, Math.floor(t * 18)) * SCRAMBLE.length) | 0]), hslk(200, 40, 60, 0.75), 10);
            continue;
          }
        }
        put(x + 0.5, sy + 0.5, c, k, 10);
      }
      if (hot) addHalo(op.c + 0.5, sy + 0.5, 3.2, 160, 60, 80, 0.5);
    }
  }
  for (let si = 0; si < SECT.length; si++) {
    const S = SECT[si];
    const b = ASSETS.banners.headings[S.id];
    if (!b || bannerWidth(b) > contentW) continue;
    if (S.revealT < 0) continue;
    drawBanner({ row: S.row, col: c0, rows: b }, t, S.revealT, true);
  }
}

function revealSections(t) {
  const bottom = scrollRow + rows - 2;
  for (let i = 0; i < SECT.length; i++) {
    const S = SECT[i];
    if (S.revealT < 0 && S.row <= bottom && S.end >= scrollRow) {
      S.revealT = t;
      sfx('section', i);
    }
  }
}

function drawNav(t, introT) {
  const a = REDUCED ? 1 : clamp01((introT - 2.6) / 0.4);
  if (a <= 0) return;
  const act = activeSection();
  const hoverNav = hover && hover.nav ? hover.nav : null;
  if (scrollRow > 0) {
    const k = hslk(220, 14, 34, 0.8 * a);
    for (let x = stage0; x < stage0 + stageCols; x++) put(x + 0.5, 2.5, C_DASH, k, 50);
  }
  for (let i = 0; i < NAVREG.length; i++) {
    const R = NAVREG[i];
    let s = R.s, k;
    const hot = hoverNav === R;
    if (R.kind === 'top') {
      k = hslk(195, 55, 70, a);
      for (let j = 0; j < s.length; j++) put(R.c0 + j + 0.5, R.row + 0.5, s.charCodeAt(j), k, 60);
      if (Math.floor(t * 1.6) % 2 === 0) put(R.c0 + s.length + 1.5, R.row + 0.5, C_HASH, hslk(195, 55, 70, a), 60);
      continue;
    }
    if (R.kind === 'nav') {
      const on = R.idx === act;
      if (!R.menu) s = on ? '[' + R.s + ']' : ' ' + R.s + ' ';
      k = on ? hslk(44, 66, 78, a) : hot ? hslk(195, 60, 82, a) : hslk(220, 14, 66, a);
      if (on && !R.menu) addHalo(R.c0 + s.length / 2, R.row + 0.5, 3, 44, 60, 80, 0.35);
    } else k = hot ? hslk(195, 60, 82, a) : hslk(220, 14, 62, a);
    for (let j = 0; j < s.length; j++) {
      const c = s.charCodeAt(j);
      if (c !== 32) put(R.c0 + j + 0.5, R.row + 0.5, c, k, 60);
    }
    if (hot) addHalo(R.c0 + 0.5, R.row + 0.5, 2.6, 160, 60, 80, 0.45);
  }
}

function drawHero(t, introT) {
  if (scrollRow > heroRows) return;
  if (!REDUCED && introT < 0.65) {
    const y = (introT - 0.05) / 0.55 * rows;
    const k = hslk(200, 60, 80, 0.7);
    for (let x = stage0; x < stage0 + stageCols; x++) put(x + 0.5, y, C_DASH, k, 30);
  }
  drawSupernova(t, introT);
  for (let i = 0; i < HERO.banner.length; i++) drawBanner(HERO.banner[i], introT, 1.25 + i * 0.12, false);
  drawTextAt(HERO.title.row, HERO.title.col, HERO.title.s, hslk(44, 40, 88), 20, 1.9, introT);
  for (let i = 0; i < HERO.tagline.length; i++) {
    const L = HERO.tagline[i];
    drawTextAt(L.row, L.col, L.s, hslk(220, 16, 74), 20, 2.2 + i * 0.25, introT);
  }
  drawTextAt(HERO.prompt.row, HERO.prompt.col, HERO.prompt.s, hslk(195, 55, 66), 20, 2.5, introT);
  if (introT > 2.9 && scrollRow < 2) {
    const sy = HERO.hint.row - scrollRow + (REDUCED ? 0 : Math.round(Math.sin(t * 3) * 0.6));
    put(HERO.hint.col + 0.5, sy + 0.5, code('v'), hslk(195, 55, 72, 0.8), 20);
  }
}

/* ------------------------------------------------------------------ */
/* main loop                                                           */
/* ------------------------------------------------------------------ */

const T0 = performance.now();
let lastNow = T0;
function frame() {
  requestAnimationFrame(frame);
  if (document.documentElement.className !== 'js') return;
  const now = performance.now();
  const dt = Math.min(0.1, Math.max(0, (now - lastNow) / 1000));
  lastNow = now;
  const t = (now - T0) / 1000;
  const introT = REDUCED ? 99 : t;
  scrollPos += (scrollTarget - scrollPos) * (1 - Math.exp(-dt * 12));
  if (Math.abs(scrollTarget - scrollPos) < 0.02) scrollPos = scrollTarget;
  scrollRow = Math.round(scrollPos);
  chr.fill(0); dep.fill(-1e9); haloN = 0;
  navLayout();
  revealSections(t);
  drawStars(t);
  drawHero(t, introT);
  drawDoc(t);
  drawNav(t, introT);
  applyHalos(Math.floor(t * 9));
  render();
}

layout();
buildDoc();
navLayout();
if (location.hash && location.hash.length > 1) {
  if (scrollToSection(location.hash.slice(1))) scrollPos = scrollTarget;
}
requestAnimationFrame(frame);
