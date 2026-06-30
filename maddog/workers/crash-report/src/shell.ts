// Shared page shell + UI kit. Visual language mirrors site/src/styles/global.css
// — white, blue accent, large radii; crash stacks use the site's dark terminal.

export function esc(s: unknown): string {
  return String(s).replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

const LOGO = `<svg viewBox="0 0 64 64" class="logo" role="img" aria-label="Maddog logo"><image href="data:image/png;base64,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" width="64" height="64"/></svg>`;

const CSS = `
:root{--accent:oklch(0.55 0.17 257);--accent-soft:color-mix(in oklch,var(--accent) 8%,white);
--bg-soft:oklch(0.976 0.0025 247);--ink:oklch(0.25 0.006 260);--ink-2:oklch(0.47 0.008 260);
--ink-3:oklch(0.6 0.008 260);--line:oklch(0.925 0.004 247);--term-bg:oklch(0.25 0.006 260);
--shadow-1:0 1px 2px oklch(0.35 0.01 260/0.18),0 1px 3px 1px oklch(0.35 0.01 260/0.08);
--sans:"Outfit","Helvetica Neue","PingFang SC","Microsoft YaHei",sans-serif;
--mono:"JetBrains Mono","SF Mono",Menlo,Consolas,monospace}
*{margin:0;padding:0;box-sizing:border-box}
html{scroll-behavior:smooth}
body{font-family:var(--sans);background:#fff;color:var(--ink);-webkit-font-smoothing:antialiased}
.wrap{max-width:1060px;margin:0 auto;padding:0 28px 64px}
header{display:flex;align-items:center;gap:10px;height:68px}
.logo{width:28px;height:28px;border-radius:8px}
header .brand{font-size:17px;font-weight:600;color:var(--ink);text-decoration:none}
header .crumb{color:var(--ink-3);font-size:15px}
header .nav{display:flex;align-items:center;gap:14px}
.navlink{color:var(--accent);text-decoration:none;font-size:14px;font-weight:500}
.navlink:hover{text-decoration:underline}
.site-nav{display:flex;align-items:center;gap:6px;margin:4px 0 10px}
.site-nav a{display:inline-flex;align-items:center;min-height:34px;padding:7px 13px;border-radius:999px;color:var(--ink-2);text-decoration:none;font-size:14px;font-weight:600}
.site-nav a:hover{background:var(--bg-soft);color:var(--ink)}
.site-nav a.active,.site-nav a[aria-current="page"]{background:var(--accent-soft);color:var(--accent)}
.chip{display:inline-flex;align-items:center;gap:8px;font-size:13.5px;color:var(--ink-2)}
.lang-switch{margin-left:auto;display:inline-flex;align-items:center;gap:2px;padding:3px;border:1px solid var(--line);border-radius:12px;background:var(--bg-soft)}
.lang-switch button{font:inherit;font-size:12.5px;font-weight:700;color:var(--ink-2);background:transparent;border:0;border-radius:9px;padding:5px 10px;cursor:pointer}
.lang-switch button.active{background:#fff;color:var(--accent);box-shadow:var(--shadow-1)}
body[data-lang="en"] [data-i18n="zh"],body[data-lang="zh"] [data-i18n="en"]{display:none!important}
.badge{font-size:11px;font-weight:600;letter-spacing:.02em;text-transform:uppercase;padding:2px 8px;border-radius:999px}
.badge.pending{background:oklch(0.95 0.04 80);color:oklch(0.5 0.12 80)}
.badge.viewer{background:var(--accent-soft);color:var(--accent)}
.badge.admin{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
h1{font-size:30px;font-weight:600;letter-spacing:-0.02em;margin:18px 0 6px}
.sub{color:var(--ink-2);font-size:14.5px;margin-bottom:28px}
.hero-line{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-top:6px}
.hero-line .sub{margin-bottom:18px}
.segmented{display:inline-flex;align-items:center;gap:3px;padding:3px;border:1px solid var(--line);border-radius:13px;background:var(--bg-soft);white-space:nowrap}
.segmented a{display:inline-flex;align-items:center;justify-content:center;min-height:30px;padding:6px 11px;border-radius:10px;color:var(--ink-2);text-decoration:none;font-size:12.5px;font-weight:700}
.segmented a.active,.segmented a[aria-current="true"]{background:#fff;color:var(--accent);box-shadow:var(--shadow-1)}
.overview-grid{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:10px;margin:0 0 20px}
.overview-card{border:1px solid var(--line);border-radius:16px;background:linear-gradient(180deg,#fff,var(--bg-soft));padding:14px 15px;min-width:0;text-decoration:none}
.overview-card:hover{border-color:color-mix(in oklch,var(--accent) 36%,var(--line));transform:translateY(-1px)}
.overview-card span,.overview-card small{display:block;color:var(--ink-3);font-size:12.5px;line-height:1.3}
.overview-card strong{display:block;color:var(--ink);font-family:var(--mono);font-size:24px;line-height:1.15;margin:6px 0 4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.overview-card.good{border-color:oklch(0.88 0.05 150);background:oklch(0.985 0.015 150)}
.overview-card.warn{border-color:oklch(0.9 0.07 80);background:oklch(0.985 0.018 80)}
.overview-card.bad{border-color:oklch(0.89 0.06 25);background:oklch(0.985 0.016 25)}
.module-nav{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin:0 0 20px}
.module-link{display:grid;grid-template-columns:1fr auto;grid-template-areas:"label value" "note note";gap:4px 10px;border:1px solid var(--line);border-radius:16px;background:#fff;color:var(--ink);text-decoration:none;padding:13px 14px;box-shadow:var(--shadow-1)}
.module-link:hover{border-color:color-mix(in oklch,var(--accent) 36%,var(--line));background:var(--accent-soft)}
.module-link.active,.module-link[aria-current="page"]{border-color:color-mix(in oklch,var(--accent) 45%,var(--line));background:var(--accent-soft)}
.module-link span{grid-area:label;color:var(--ink-2);font-size:13px;font-weight:700}
.module-link b{grid-area:value;font-family:var(--mono);font-size:13px;color:var(--accent)}
.module-link small{grid-area:note;color:var(--ink-3);font-size:12.5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}
.card{background:#fff;border:1px solid var(--line);border-radius:24px;padding:24px 28px;box-shadow:var(--shadow-1)}
.card.full{grid-column:1/-1}
.card h2{font-size:15px;font-weight:600;color:var(--ink-2);margin-bottom:16px}
.card h2 b{color:var(--ink)}
.module-card{scroll-margin-top:18px}
.module-card+.module-card{margin-top:2px}
.module-head{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:16px}
.module-head span{display:block;font-size:11px;text-transform:uppercase;letter-spacing:.08em;color:var(--ink-3);font-weight:800;margin-bottom:4px}
.module-head h2{margin:0;color:var(--ink);font-size:19px}
.module-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;flex-wrap:wrap}
.module-action{color:var(--accent);text-decoration:none;font-size:13.5px;font-weight:700;white-space:nowrap}
.module-action:hover{text-decoration:underline}
.module-panel{border:1px solid var(--line);border-radius:18px;background:var(--bg-soft);padding:16px 18px;margin-top:12px;min-width:0}
.module-panel.wide{margin-top:0}
.module-panel h3{font-size:13px;font-weight:700;color:var(--ink-2);margin:0 0 12px}
.module-panel h3 b{color:var(--ink)}
.module-split{display:grid;grid-template-columns:1fr 1fr;gap:12px;align-items:start}
.module-split.wide{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}
.card-title-row{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:16px}
.card-title-row h2{margin:0}
.empty{color:var(--ink-3);font-size:14px;padding:14px 0}
svg.chart{width:100%;height:auto;display:block}
.bars-list{min-width:0}
.row{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(76px,1fr) minmax(3.8em,max-content);align-items:center;gap:12px;padding:7px 0;font-size:14px;min-width:0}
.row+.row{border-top:1px solid var(--line)}
.row-label{display:flex;align-items:center;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row-bar{min-width:0}
.row .bar{height:10px;border-radius:999px;background:linear-gradient(90deg,#4f9dff,#7a6bff);min-width:4px}
.row .n{text-align:right;font-family:var(--mono);font-size:13px;color:var(--ink-2);white-space:nowrap}
.bucket-prefix{display:inline-flex;align-items:center;margin-right:6px;padding:1px 6px;border-radius:8px;background:var(--accent-soft);color:var(--accent);font-size:12px;font-weight:700}
.bucket-main{display:inline-block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.bars-more{margin-top:6px}
.bars-more summary{display:inline-flex;align-items:center;min-height:30px;padding:5px 9px;border:1px dashed var(--line);border-radius:10px;background:#fff;color:var(--accent);font-size:12.5px;font-weight:700;cursor:pointer;list-style:none}
.bars-more summary::-webkit-details-marker{display:none}
.bars-more summary:hover{border-color:color-mix(in oklch,var(--accent) 34%,var(--line))}
.bars-more .more-open{display:none}
.bars-more[open] .more-closed{display:none}
.bars-more[open] .more-open{display:inline}
.bars-more-list{max-height:260px;overflow-y:auto;overflow-x:hidden;margin-top:7px;padding:5px 8px;border:1px solid var(--line);border-radius:12px;background:#fff}
table{border-collapse:collapse;width:100%;font-size:14px}
th{text-align:left;color:var(--ink-3);font-weight:500;font-size:12.5px;padding:0 14px 8px 0}
td{padding:8px 14px 8px 0;border-top:1px solid var(--line);vertical-align:middle}
td.n{font-family:var(--mono);font-size:13px}
td.summary{max-width:480px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-family:var(--mono);font-size:12.5px}
.muted{color:var(--ink-3)}
p.summary{font-family:var(--mono);font-size:14px;margin:2px 0 6px;word-break:break-word}
a.fp{font-family:var(--mono);font-size:13px;color:var(--accent);text-decoration:none}
a.fp:hover{text-decoration:underline}
.pill{display:inline-block;font-size:12px;font-weight:500;padding:2px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent)}
.pill.crash{background:oklch(0.95 0.03 25);color:oklch(0.55 0.18 25)}
.pill.open{background:oklch(0.96 0.025 250);color:var(--ink-2)}
.pill.resolved{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
.pill.ignored{background:var(--bg-soft);color:var(--ink-3)}
.back{display:inline-block;margin:18px 0 0;color:var(--accent);text-decoration:none;font-size:14px}
.report{margin-top:20px}
.report .meta{display:flex;flex-wrap:wrap;gap:8px 18px;font-size:13px;color:var(--ink-2);margin-bottom:10px}
.report .meta b{color:var(--ink);font-weight:500}
pre{background:var(--term-bg);color:#e6e8f0;border-radius:12px;padding:16px 18px;font-family:var(--mono);
font-size:12.5px;line-height:1.55;overflow:auto;white-space:pre-wrap;word-break:break-word}
.metrics{display:grid;grid-template-columns:1fr 1fr;gap:8px 28px}
.metric-block h3{display:flex;align-items:center;justify-content:space-between;gap:8px;font-family:var(--mono);font-size:12.5px;font-weight:600;color:var(--ink-2);margin:6px 0 4px}
.metric-block h3 span{font-family:var(--mono);font-size:11px;color:var(--ink-3);font-weight:600}
.preference-dashboard{display:flex;flex-direction:column;gap:14px}
.preference-compare{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;align-items:start}
.preference-panel{background:#fff}
.preference-panel.active{border-color:color-mix(in oklch,var(--accent) 42%,var(--line));box-shadow:0 0 0 3px var(--accent-soft)}
.pref-section{border:1px solid var(--line);border-radius:14px;background:#fff;padding:14px 15px}
.pref-section>h3,.pref-section summary h3{display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:13px;font-weight:700;color:var(--ink);margin:0 0 10px}
.pref-section>h3 span,.pref-section summary h3 span{font-size:11.5px;color:var(--ink-3);font-weight:700;white-space:nowrap}
.pref-section summary{cursor:pointer;list-style:none}
.pref-section summary::-webkit-details-marker{display:none}
.pref-section summary h3{margin:0}
.pref-section summary h3::after{content:"+";font-family:var(--mono);font-size:13px;color:var(--accent);line-height:1}
.pref-section[open] summary h3::after{content:"-"}
.pref-section[open] summary h3{margin-bottom:10px}
.pref-section-collapsed summary:hover h3{color:var(--accent)}
.pref-section .metric-block h3{color:var(--ink-3)}
.pref-metrics{grid-template-columns:1fr;gap:12px}
.pref-metrics .metric-block{min-width:0}
.pref-metrics .row{grid-template-columns:minmax(7rem,1.2fr) minmax(5rem,.9fr) minmax(4.4em,max-content)}
.health-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px}
.health-card{border:1px solid var(--line);border-radius:16px;background:var(--bg-soft);padding:14px 15px;min-width:0}
.health-card.good{background:oklch(0.985 0.015 150);border-color:oklch(0.88 0.05 150)}
.health-card.warn{background:oklch(0.985 0.018 80);border-color:oklch(0.9 0.07 80)}
.health-card.bad{background:oklch(0.985 0.016 25);border-color:oklch(0.89 0.06 25)}
.health-top{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:8px}
.health-top span{font-size:12.5px;color:var(--ink-2);font-weight:700}
.health-top b{font-size:11px;text-transform:uppercase;color:var(--ink-3);font-weight:800}
.health-card strong{display:block;font-family:var(--mono);font-size:22px;color:var(--ink);margin-bottom:4px}
.health-card small{display:block;color:var(--ink-3);font-family:var(--mono);font-size:11.5px;margin-bottom:9px}
.health-card p{color:var(--ink-2);font-size:12.5px;line-height:1.35;word-break:break-word}
.filter-card{margin-top:12px;padding:16px 18px;border:1px solid var(--line);border-radius:18px;background:#fff;position:sticky;top:10px;z-index:2}
.filter-head{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:14px}
.filter-head h2{margin:0}
.filter-head span{font-family:var(--mono);font-size:12.5px;color:var(--ink-3);white-space:nowrap}
.filter-tabs{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:18px}
.filter-tab{display:inline-flex;align-items:center;min-height:34px;padding:7px 13px;border-radius:12px;border:1px solid var(--line);
background:var(--bg-soft);color:var(--ink-2);text-decoration:none;font-size:13.5px;font-weight:600}
.filter-tab:hover{border-color:color-mix(in oklch,var(--accent) 34%,var(--line));color:var(--accent)}
.filter-tab.active{background:var(--accent);border-color:var(--accent);color:#fff}
.facet-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}
.facet-grid section{min-width:0}
.facet-grid h3{font-size:12px;font-weight:600;color:var(--ink-3);margin:0 0 8px;text-transform:uppercase}
.facet-list{display:flex;flex-wrap:wrap;gap:7px;min-width:0}
.facet-chip{display:inline-grid;grid-template-columns:minmax(0,auto) auto;align-items:center;gap:8px;max-width:100%;min-width:0;
padding:6px 9px;border:1px solid var(--line);border-radius:11px;background:#fff;color:var(--ink-2);text-decoration:none;font-size:13px}
.facet-chip:hover{border-color:color-mix(in oklch,var(--accent) 30%,var(--line));color:var(--accent)}
.facet-chip.active{background:var(--accent-soft);border-color:color-mix(in oklch,var(--accent) 45%,var(--line));color:var(--accent)}
.facet-chip b{font-family:var(--mono);font-size:12px;color:inherit}
.facet-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}
.facet-more{flex-basis:100%;min-width:0}
.facet-more summary{display:inline-flex;align-items:center;min-height:32px;padding:6px 10px;border:1px dashed var(--line);border-radius:11px;background:var(--bg-soft);color:var(--accent);font-size:13px;font-weight:700;cursor:pointer;list-style:none}
.facet-more summary::-webkit-details-marker{display:none}
.facet-more summary:hover{border-color:color-mix(in oklch,var(--accent) 34%,var(--line))}
.facet-more-list{display:flex;flex-wrap:wrap;gap:7px;max-height:150px;overflow:auto;margin-top:8px;padding:8px;border:1px solid var(--line);border-radius:13px;background:#fff}
.filter-empty{font-size:13px;color:var(--ink-3)}
.crash-card{overflow:hidden}
.crash-list{display:flex;flex-direction:column;gap:2px}
.crash-head,.crash-item{display:grid;grid-template-columns:minmax(300px,1fr) minmax(185px,.55fr) 150px 62px;gap:16px;align-items:center}
.crash-head{padding:0 12px 9px;color:var(--ink-3);font-size:12.5px;font-weight:600;border-bottom:1px solid var(--line)}
.crash-item{margin:0 -12px;padding:14px 12px;border-radius:14px;color:var(--ink);text-decoration:none;border-bottom:1px solid var(--line)}
.crash-item:hover{background:var(--bg-soft)}
.crash-fingerprint{display:flex;flex-direction:column;gap:3px;min-width:0}
.crash-fingerprint b{font-family:var(--mono);font-size:13px;color:var(--accent);font-weight:700}
.crash-fingerprint small,.crash-scope small{font-family:var(--mono);font-size:11.5px;color:var(--ink-3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.crash-summary{display:flex;flex-direction:column;gap:5px;min-width:0}
.crash-summary>span{font-family:var(--mono);font-size:12.8px;line-height:1.45;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.crash-summary small{font-family:var(--mono);font-size:11.5px;color:var(--ink-3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.crash-summary em{font-size:12px;font-style:normal;color:oklch(0.55 0.18 25)}
.crash-scope{display:flex;flex-direction:column;gap:4px;min-width:0}
.crash-health{display:flex;flex-wrap:wrap;gap:6px;align-items:center}
.crash-count{font-family:var(--mono);font-size:14px;font-weight:700;color:var(--ink);text-align:right}
.group-hero{padding:28px 0 22px}
.group-nav{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:18px}
.group-nav .back{margin:0}
.group-title{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:8px}
.group-title h1{margin:0;font-family:var(--mono);font-size:34px}
.group-summary{font-size:17px;margin:0 0 12px}
.group-tags{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 18px}
.group-tags span{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:11px;background:#fff;padding:6px 9px;font-size:13px;color:var(--ink-2)}
.group-tags b{font-size:12px;color:var(--ink-3);font-weight:600}
.crash-list.compact .crash-head{display:none}
.crash-list.compact .crash-item{padding:12px;margin:0 -12px;grid-template-columns:minmax(260px,1fr) minmax(160px,.5fr) 140px 52px}
.group-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}
.group-metrics div{border:1px solid var(--line);border-radius:14px;background:var(--bg-soft);padding:12px 13px;min-width:0}
.group-metrics span{display:block;font-size:12px;color:var(--ink-3);margin-bottom:5px}
.group-metrics b{display:block;font-family:var(--mono);font-size:13px;color:var(--ink);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.group-note{margin-top:12px;color:var(--ink-2);font-size:13.5px}
.sample-card h2{display:flex;align-items:center;justify-content:space-between;gap:12px}
.sample-list{display:flex;flex-direction:column;gap:10px}
.sample{border:1px solid var(--line);border-radius:16px;background:#fff;overflow:hidden}
.sample summary{display:grid;grid-template-columns:140px minmax(0,1fr) 170px;gap:14px;align-items:center;padding:14px 16px;cursor:pointer;list-style:none}
.sample summary::-webkit-details-marker{display:none}
.sample summary:hover{background:var(--bg-soft)}
.sample[open] summary{border-bottom:1px solid var(--line)}
.sample-id{display:flex;flex-direction:column;gap:3px;min-width:0}
.sample-id b{font-family:var(--mono);font-size:14px;color:var(--ink)}
.sample-id small,.sample-time{font-family:var(--mono);font-size:12px;color:var(--ink-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sample-title{font-family:var(--mono);font-size:12.8px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sample-time{text-align:right}
.sample-body{padding:14px 16px 16px}
.sample-meta{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px}
.sample-meta span{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:10px;padding:5px 8px;color:var(--ink-2);font-size:12.5px}
.sample-meta b{color:var(--ink-3);font-weight:600}
.sample-actions{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px}
.sample-more{border:1px dashed var(--line);border-radius:16px;background:var(--bg-soft);padding:10px}
.sample-more>summary{cursor:pointer;list-style:none;color:var(--accent);font-size:13.5px;font-weight:700;padding:4px 6px}
.sample-more>summary::-webkit-details-marker{display:none}
.sample-more-list{display:flex;flex-direction:column;gap:10px;margin-top:10px}
.copy-btn[data-state="copied"]{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
.copy-btn[data-state="copied"] .copy-label{display:none}
.copy-btn[data-state="copied"]::after{content:"Copied"}
body[data-lang="zh"] .copy-btn[data-state="copied"]::after{content:"已复制"}
.copy-btn[data-state="failed"]{background:oklch(0.96 0.03 25);color:oklch(0.55 0.18 25)}
.copy-btn[data-state="failed"] .copy-label{display:none}
.copy-btn[data-state="failed"]::after{content:"Copy failed"}
body[data-lang="zh"] .copy-btn[data-state="failed"]::after{content:"复制失败"}
.sample-nested{margin-top:10px}
.sample-nested summary{cursor:pointer;color:var(--accent);font-size:13px;margin-bottom:8px}
.sample-nested pre{margin-top:8px}
.manage-card{margin-top:20px}
.manage-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}
.manage-head h2{margin:0}
.manage-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}
.manage-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.manage-form{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px;align-items:end}
.manage-form.wide{grid-column:1/-1}
.manage-form label{display:flex;flex-direction:column;gap:6px;font-size:12.5px;color:var(--ink-3);font-weight:600}
.manage-form input,.manage-form select{min-width:0}
form.inline{display:inline}
.authwrap{max-width:400px;margin:6vh auto 0}
.authcard{background:#fff;border:1px solid var(--line);border-radius:24px;padding:32px 34px;box-shadow:var(--shadow-1)}
.authcard h1{font-size:23px;margin:0 0 4px}
.authcard .sub{margin-bottom:22px}
.field{display:block;margin-bottom:14px}
.field label{display:block;font-size:13px;color:var(--ink-2);margin-bottom:6px}
.field input,select{width:100%;font:inherit;font-size:14.5px;color:var(--ink);background:#fff;border:1px solid var(--line);border-radius:12px;padding:10px 13px;outline:none}
.field input:focus,select:focus{border-color:var(--accent)}
.btn{font:inherit;font-size:14.5px;font-weight:600;color:#fff;background:var(--accent);border:none;border-radius:12px;padding:10px 18px;cursor:pointer}
.btn:hover{filter:brightness(1.05)}
.btn.block{width:100%}
.btn.ghost{background:var(--accent-soft);color:var(--accent)}
.btn.danger{background:oklch(0.95 0.03 25);color:oklch(0.55 0.18 25)}
.btn.sm{font-size:13px;padding:6px 12px;border-radius:10px}
.notice{font-size:13.5px;padding:10px 14px;border-radius:12px;margin-bottom:16px}
.notice.err{background:oklch(0.96 0.03 25);color:oklch(0.5 0.18 25)}
.notice.ok{background:oklch(0.95 0.05 150);color:oklch(0.45 0.13 150)}
.alt{margin-top:18px;font-size:13.5px;color:var(--ink-3);text-align:center}
.alt a{color:var(--accent);text-decoration:none}
.actions{display:flex;gap:8px;align-items:center}
td select{width:auto;padding:6px 10px;border-radius:10px}
.note-edit{margin-top:12px;display:flex;gap:8px;max-width:560px}
.note-edit input{flex:1}
@media(max-width:1100px){.overview-grid{grid-template-columns:repeat(3,minmax(0,1fr))}.module-nav{grid-template-columns:repeat(2,minmax(0,1fr))}.health-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.module-split,.module-split.wide,.preference-compare{grid-template-columns:1fr}}
@media(max-width:900px){.hero-line{flex-direction:column}.facet-grid{grid-template-columns:1fr}.crash-head{display:none}.crash-item,.crash-list.compact .crash-item{grid-template-columns:1fr;gap:10px}
.crash-health{justify-content:flex-start}.crash-count{text-align:left}.filter-head,.module-head{align-items:flex-start;flex-direction:column;gap:8px}.module-actions{justify-content:flex-start}
.group-metrics{grid-template-columns:1fr 1fr}.sample summary{grid-template-columns:1fr}.sample-time{text-align:left}.manage-head{flex-direction:column}.manage-actions{justify-content:flex-start}.manage-grid{grid-template-columns:1fr}.manage-form.wide{grid-column:auto}}
@media(max-width:760px){header{height:auto;min-height:68px;flex-wrap:wrap;padding:14px 0}.lang-switch{margin-left:0}.nav{width:100%;flex-wrap:wrap;gap:8px}.site-nav{overflow-x:auto;padding-bottom:2px}.site-nav a{white-space:nowrap}.chip{min-width:0;max-width:100%;overflow:hidden;text-overflow:ellipsis}.grid{grid-template-columns:1fr}.overview-grid,.module-nav,.health-grid{grid-template-columns:1fr}.metrics,.pref-metrics{grid-template-columns:1fr}.row{grid-template-columns:minmax(0,1fr) minmax(70px,.8fr) 3.2em}.card-title-row{align-items:flex-start;flex-direction:column}.filter-card{position:static}.wrap{padding:0 16px 48px}}
`;

const LANG_SWITCH = `<span class="lang-switch" role="group" aria-label="Language"><button type="button" data-lang-option="zh">中文</button><button type="button" data-lang-option="en">EN</button></span>`;

const LANG_SCRIPT = `<script>(()=>{const k="maddog.stats.lang";const b=[...document.querySelectorAll("[data-lang-option]")];const pick=()=>{try{return localStorage.getItem(k)}catch{return""}};const fallback=()=>((navigator.language||"").toLowerCase().startsWith("zh")?"zh":"en");const set=(v)=>{const lang=v==="en"?"en":"zh";document.body.dataset.lang=lang;b.forEach((x)=>{const on=x.dataset.langOption===lang;x.classList.toggle("active",on);x.setAttribute("aria-pressed",String(on))});try{localStorage.setItem(k,lang)}catch{}};b.forEach((x)=>x.addEventListener("click",()=>set(x.dataset.langOption||"zh")));set(pick()||fallback())})();(()=>{const fallback=(t)=>{const a=document.createElement("textarea");a.value=t;a.setAttribute("readonly","");a.style.position="fixed";a.style.opacity="0";document.body.appendChild(a);a.select();let ok=false;try{ok=document.execCommand("copy")}catch{}a.remove();return ok};document.addEventListener("click",async(e)=>{const b=e.target.closest("[data-copy]");if(!b)return;const t=b.dataset.copy||"";let ok=false;try{if(navigator.clipboard){await navigator.clipboard.writeText(t);ok=true}}catch{}if(!ok)ok=fallback(t);b.dataset.state=ok?"copied":"failed";setTimeout(()=>{delete b.dataset.state},1400)})})()</script>`;

const STATS_ROUTE_SCRIPT = `<script>(()=>{if(location.pathname!=="/stats"||!location.hash)return;const m={diagnostics:"diagnostics",usage:"",preferences:"preferences",health:"health"};const key=location.hash.slice(1);if(m[key]===undefined)return;location.replace("/stats"+(m[key]?"/"+m[key]:"")+location.search)})()</script>`;

export function page(title: string, crumb: string, body: string, nav = ""): string {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>${esc(title)}</title><style>${CSS}</style></head><body><div class="wrap">
<header><a class="brand" href="https://maddog.io">${LOGO}</a><a class="brand" href="https://maddog.io">Maddog</a><span class="crumb">/ ${esc(crumb)}</span>${LANG_SWITCH}${nav ? `<span class="nav">${nav}</span>` : ""}</header>
${body}</div>${LANG_SCRIPT}</body></html>`;
}

export function html(body: string): Response {
  return new Response(body, { headers: { "content-type": "text/html; charset=utf-8" } });
}

export function redirect(location: string, cookie?: string): Response {
  const headers: Record<string, string> = { location };
  if (cookie) headers["set-cookie"] = cookie;
  return new Response(null, { status: 303, headers });
}
