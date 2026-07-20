package web

import "net/http"

func registerAssets(r interface {
	Get(string, http.HandlerFunc)
}) {
	r.Get("/static/app.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write([]byte(appCSS))
	})
	r.Get("/static/app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(appJS))
	})
}

const appJS = `
document.addEventListener("change", (event) => {
  if (event.target.matches("[data-cluster-switcher]")) {
    window.location.assign(event.target.value);
  }
});

document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  const value = button.closest(".copy-field")?.querySelector("code")?.textContent;
  if (!value) return;
  const original = button.textContent;
  try {
    await navigator.clipboard.writeText(value);
    button.textContent = "Copied";
  } catch {
    button.textContent = "Copy failed";
  }
  window.setTimeout(() => { button.textContent = original; }, 1400);
});

document.addEventListener("input", (event) => {
  if (!event.target.matches("[data-tenant-filter]")) return;
  const query = event.target.value.trim().toLowerCase();
  document.querySelectorAll("[data-tenant-row]").forEach((row) => {
    row.hidden = query !== "" && !row.textContent.toLowerCase().includes(query);
  });
});
`

const appCSS = `
:root {
  color-scheme: light;
  --bg: #f5f7fb;
  --surface: #ffffff;
  --surface-soft: #f8fafc;
  --text: #172033;
  --muted: #65718a;
  --line: #dfe5ef;
  --primary: #3157d5;
  --primary-dark: #2444b5;
  --danger: #b42318;
  --danger-soft: #fff1f0;
  --success: #16794a;
  --radius: 14px;
  --shadow: 0 8px 24px rgba(21, 32, 58, .07);
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  background: var(--bg);
  color: var(--text);
}
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; background: var(--bg); }
a { color: var(--primary); text-decoration: none; }
a:hover { text-decoration: underline; }
code, pre { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
code { background: #edf1f8; border-radius: 6px; padding: .12rem .35rem; }
.skip-link { position: fixed; left: .75rem; top: -4rem; z-index: 100; background: var(--text); color: #fff; padding: .6rem .9rem; border-radius: 8px; }
.skip-link:focus { top: .75rem; }
.app-bar { position: sticky; top: 0; z-index: 20; background: rgba(255,255,255,.96); border-bottom: 1px solid var(--line); backdrop-filter: blur(12px); }
.app-bar__inner { max-width: 82rem; margin: 0 auto; min-height: 68px; padding: .75rem 1.25rem; display: flex; align-items: center; gap: 1.25rem; }
.brand { display: flex; align-items: center; gap: .7rem; color: var(--text); font-weight: 760; letter-spacing: -.02em; white-space: nowrap; }
.brand:hover { text-decoration: none; }
.brand__mark { display: grid; place-items: center; width: 34px; height: 34px; color: #fff; border-radius: 10px; background: linear-gradient(145deg, #3157d5, #6037bd); font-size: .78rem; letter-spacing: 0; }
.app-bar__spacer { flex: 1; }
.cluster-switch { display: grid; gap: .2rem; min-width: min(22rem, 42vw); }
.cluster-switch label { color: var(--muted); font-size: .7rem; font-weight: 700; text-transform: uppercase; letter-spacing: .08em; }
select, input, button { font: inherit; }
select, input[type=text], input[type=search], input[type=url], input[type=file] { width: 100%; min-height: 42px; border: 1px solid #cbd4e3; border-radius: 9px; background: #fff; color: var(--text); padding: .58rem .72rem; }
select:focus, input:focus, button:focus, a:focus { outline: 3px solid rgba(49,87,213,.2); outline-offset: 2px; }
.identity { color: var(--muted); font-size: .82rem; text-align: right; white-space: nowrap; }
.identity strong { display: block; max-width: 15rem; overflow: hidden; text-overflow: ellipsis; color: var(--text); }
.shell { max-width: 82rem; margin: 0 auto; padding: 2rem 1.25rem 4rem; }
.page-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 1.5rem; margin-bottom: 1.5rem; }
h1, h2, h3, p { margin-top: 0; }
h1 { font-size: clamp(1.7rem, 3vw, 2.25rem); letter-spacing: -.035em; margin-bottom: .35rem; }
h2 { font-size: 1.15rem; letter-spacing: -.015em; }
.muted { color: var(--muted); }
.eyebrow { margin-bottom: .35rem; color: var(--primary); font-size: .76rem; font-weight: 750; text-transform: uppercase; letter-spacing: .08em; }
.toolbar { display: flex; align-items: center; justify-content: flex-end; gap: .65rem; flex-wrap: wrap; }
.button, button { display: inline-flex; align-items: center; justify-content: center; min-height: 40px; border: 1px solid transparent; border-radius: 9px; padding: .55rem .85rem; font-weight: 700; cursor: pointer; background: var(--primary); color: #fff; text-decoration: none; }
.button:hover, button:hover { background: var(--primary-dark); text-decoration: none; }
.button.secondary, button.secondary { border-color: #cbd4e3; background: #fff; color: var(--text); }
.button.secondary:hover, button.secondary:hover { background: var(--surface-soft); }
.button.danger { border-color: #fecaca; background: var(--danger-soft); color: var(--danger); }
.button.small { min-height: 34px; padding: .35rem .62rem; font-size: .82rem; }
.cluster-summary { display: grid; grid-template-columns: repeat(4, minmax(0,1fr)); gap: 1rem; margin: 0 0 1.5rem; }
.metric, article, .card { border: 1px solid var(--line); border-radius: var(--radius); background: var(--surface); box-shadow: var(--shadow); }
.metric { padding: 1rem 1.1rem; }
.metric dt { color: var(--muted); font-size: .76rem; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; }
.metric dd { margin: .35rem 0 0; font-size: 1.03rem; font-weight: 700; overflow-wrap: anywhere; }
.table-card { overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius); background: var(--surface); box-shadow: var(--shadow); }
.table-tools { display: flex; justify-content: space-between; align-items: center; gap: 1rem; padding: 1rem; border-bottom: 1px solid var(--line); }
.table-tools input { max-width: 23rem; }
.table-scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; min-width: 680px; }
th, td { padding: .8rem 1rem; border-bottom: 1px solid var(--line); text-align: left; vertical-align: middle; }
th { background: var(--surface-soft); color: var(--muted); font-size: .73rem; text-transform: uppercase; letter-spacing: .055em; }
tbody tr:last-child td { border-bottom: 0; }
tbody tr:hover { background: #f9fbff; }
.database-name { color: var(--text); font-weight: 720; }
.badge { display: inline-flex; align-items: center; border-radius: 999px; padding: .2rem .5rem; background: #eaf0ff; color: #294bb9; font-size: .72rem; font-weight: 750; }
.badge.viewer { background: #edf1f8; color: var(--muted); }
.cards { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 1rem; margin-bottom: 1rem; }
article, .card { padding: 1.15rem; margin-bottom: 1rem; }
article > header { margin: -1.15rem -1.15rem 1rem; padding: .9rem 1.15rem; border-bottom: 1px solid var(--line); font-weight: 750; }
.kv { display: grid; grid-template-columns: minmax(8rem, 35%) 1fr; gap: .45rem .8rem; margin: 0; }
.kv dt { color: var(--muted); }
.kv dd { margin: 0; overflow-wrap: anywhere; }
.copy-field { display: grid; grid-template-columns: minmax(0,1fr) auto; gap: .65rem; align-items: start; margin-top: .65rem; }
.copy-field pre { margin: 0; padding: .75rem; overflow-x: auto; border-radius: 9px; background: #111827; color: #e5e7eb; }
.danger-zone { border-color: #fecaca; background: #fffafa; }
.danger-zone > header { color: var(--danger); }
.form-card { max-width: 44rem; margin: 0 auto; }
.form-actions { display: flex; justify-content: flex-end; gap: .65rem; margin-top: 1.25rem; }
.alert { border-radius: 9px; padding: .75rem .9rem; background: var(--danger-soft); color: var(--danger); }
nav[aria-label=breadcrumb] ul { display: flex; gap: .45rem; padding: 0; list-style: none; color: var(--muted); font-size: .85rem; }
nav[aria-label=breadcrumb] li + li::before { content: "/"; margin-right: .45rem; color: #a4adbc; }
footer { max-width: 82rem; margin: 0 auto; padding: 1rem 1.25rem 2rem; color: var(--muted); font-size: .8rem; }
[hidden] { display: none !important; }
@media (max-width: 760px) {
  .app-bar__inner { align-items: flex-start; flex-wrap: wrap; gap: .7rem; }
  .app-bar__spacer { display: none; }
  .cluster-switch { order: 3; width: 100%; min-width: 0; }
  .identity { margin-left: auto; }
  .shell { padding-top: 1.25rem; }
  .page-heading { display: block; }
  .toolbar { justify-content: stretch; margin-top: 1rem; }
  .toolbar > * { flex: 1 1 auto; }
  .cluster-summary { grid-template-columns: repeat(2, minmax(0,1fr)); }
  .cards { grid-template-columns: 1fr; }
  .table-tools { align-items: stretch; flex-direction: column; }
  .table-tools input { max-width: none; }
  .copy-field { grid-template-columns: 1fr; }
  .copy-field button { justify-self: stretch; }
}
@media (max-width: 480px) {
  .identity { font-size: .72rem; }
  .identity strong { max-width: 10rem; }
  .cluster-summary { grid-template-columns: 1fr; }
  .hide-mobile { display: none; }
  table { min-width: 490px; }
  th, td { padding: .7rem .75rem; }
}
`
