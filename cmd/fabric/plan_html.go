package main

import (
	"fmt"
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/architect"
)

func planToHTML(p *architect.Plan) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>fabric plan — ` + htmlEscape(p.Summary) + `</title>
<style>
  :root {
    --bg: #ffffff;
    --bg-card: #f8f9fa;
    --bg-code: #f1f3f5;
    --border: #e9ecef;
    --text: #212529;
    --text-secondary: #6c757d;
    --text-muted: #adb5bd;
    --blue: #1864ab;
    --blue-bg: #e7f5ff;
    --green: #2b8a3e;
    --green-bg: #ebfbee;
    --yellow: #e8590c;
    --yellow-bg: #fff4e6;
    --purple: #5f3dc4;
    --purple-bg: #f3f0ff;
    --red: #c92a2a;
    --red-bg: #ffe3e3;
    --radius: 8px;
    --radius-sm: 4px;
    --shadow: 0 1px 3px rgba(0,0,0,.06);
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", sans-serif;
    background: var(--bg);
    color: var(--text);
    padding: 3rem 2rem;
    max-width: 900px;
    margin: 0 auto;
    line-height: 1.65;
    font-size: 15px;
  }

  /* ── Header ── */
  .plan-header { margin-bottom: 2.5rem; }
  .plan-header h1 { font-size: 1.75rem; font-weight: 700; color: var(--text); letter-spacing: -0.3px; }
  .plan-header .subtitle { color: var(--text-secondary); margin-top: 0.25rem; display: flex; gap: 1rem; align-items: center; }

  /* ── Stats ── */
  .stats { display: flex; gap: 0.75rem; margin-bottom: 2.5rem; flex-wrap: wrap; }
  .stat {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 0.5rem 0.9rem; border-radius: 20px;
    font-size: 0.82rem; font-weight: 500;
  }
  .stat-new  { background: var(--green-bg); color: var(--green); }
  .stat-mod  { background: var(--yellow-bg); color: var(--yellow); }
  .stat-test { background: var(--purple-bg); color: var(--purple); }
  .stat-risk { background: var(--red-bg); color: var(--red); }
  .stat-pat  { background: var(--blue-bg); color: var(--blue); }

  /* ── Sections ── */
  h2 {
    font-size: 1.05rem; font-weight: 600; color: var(--text);
    margin: 2.5rem 0 0.75rem; display: flex; align-items: center; gap: 0.5rem;
  }
  h2 .count { font-weight: 400; color: var(--text-muted); font-size: 0.85rem; }
  h2::before { content: ""; display: inline-block; width: 3px; height: 1rem; border-radius: 2px; background: var(--blue); }

  /* ── File cards ── */
  .file {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1rem 1.15rem;
    margin-bottom: 0.6rem;
    box-shadow: var(--shadow);
    transition: box-shadow 0.15s;
  }
  .file:hover { box-shadow: 0 2px 8px rgba(0,0,0,.08); }
  .file-top { display: flex; align-items: baseline; gap: 0.5rem; margin-bottom: 0.35rem; flex-wrap: wrap; }
  .file-action {
    font-size: 0.68rem; font-weight: 600; text-transform: uppercase;
    padding: 0.15rem 0.45rem; border-radius: 3px; letter-spacing: 0.3px;
    flex-shrink: 0;
  }
  .action-create { background: var(--green-bg); color: var(--green); }
  .action-modify { background: var(--yellow-bg); color: var(--yellow); }
  .file-path {
    font-family: "SF Mono", "SFMono-Regular", "Menlo", "Monaco", monospace;
    font-size: 0.82rem; color: var(--text);
    word-break: break-all;
  }
  .file-purpose { color: var(--text-secondary); font-size: 0.88rem; margin-bottom: 0.6rem; }

  /* ── Spec block ── */
  .file-spec {
    background: var(--bg-code);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.65rem 0.85rem;
    margin: 0.5rem 0;
    font-size: 0.82rem;
    line-height: 1.55;
    color: var(--text);
    white-space: pre-wrap;
    font-family: "SF Mono", "SFMono-Regular", "Menlo", "Monaco", monospace;
    overflow-x: auto;
  }

  /* ── Meta tags ── */
  .file-meta { display: flex; gap: 0.75rem; flex-wrap: wrap; margin-top: 0.5rem; }
  .tag {
    display: inline-flex; align-items: center; gap: 0.3rem;
    font-size: 0.76rem; color: var(--text-secondary);
  }
  .tag::before { content: ""; display: inline-block; width: 5px; height: 5px; border-radius: 50%; background: var(--text-muted); }
  .tag-follows::before { background: var(--blue); }
  .tag-needs::before { background: var(--purple); }
  .tag-side::before { background: var(--yellow); }

  /* ── Tests ── */
  .test-item {
    display: flex; align-items: baseline; gap: 0.6rem;
    padding: 0.55rem 0; border-bottom: 1px solid var(--border);
  }
  .test-item:last-child { border-bottom: none; }
  .test-icon { font-size: 0.95rem; flex-shrink: 0; }
  .test-body { min-width: 0; }
  .test-path {
    font-family: "SF Mono", "SFMono-Regular", "Menlo", "Monaco", monospace;
    font-size: 0.82rem; color: var(--text);
  }
  .test-strategy { color: var(--text-secondary); font-size: 0.84rem; margin-top: 0.1rem; }

  /* ── Chips ── */
  .chips { display: flex; flex-wrap: wrap; gap: 0.4rem; }
  .chip {
    background: var(--blue-bg); color: var(--blue);
    font-size: 0.8rem; padding: 0.25rem 0.65rem; border-radius: 14px;
    font-weight: 500;
  }

  /* ── Lists ── */
  .item-list { list-style: none; }
  .item-row { display: flex; gap: 0.5rem; padding: 0.45rem 0; font-size: 0.9rem; align-items: baseline; }
  .item-row .icon { flex-shrink: 0; font-size: 0.9rem; }
  .risk-icon  { color: var(--yellow); }
  .check-icon { color: var(--green); }

  /* ── Architecture overview ── */
  .arch-overview { margin-bottom: 2rem; }
  .arch-bar { display: flex; height: 8px; border-radius: 4px; overflow: hidden; margin-bottom: 0.4rem; }
  .arch-seg { height: 100%; }
  .arch-seg-domain    { background: var(--purple); }
  .arch-seg-app       { background: var(--blue); }
  .arch-seg-adapters  { background: var(--green); }
  .arch-seg-deploy    { background: var(--yellow); }
  .arch-legend { display: flex; gap: 1rem; flex-wrap: wrap; font-size: 0.78rem; color: var(--text-secondary); }
  .arch-legend span::before {
    content: ""; display: inline-block; width: 8px; height: 8px;
    border-radius: 2px; margin-right: 0.3rem; vertical-align: middle;
  }
  .arch-legend .l-domain::before    { background: var(--purple); }
  .arch-legend .l-app::before       { background: var(--blue); }
  .arch-legend .l-adapters::before  { background: var(--green); }
  .arch-legend .l-deploy::before    { background: var(--yellow); }

  /* ── Footer ── */
  footer {
    margin-top: 3rem; padding-top: 1.25rem;
    border-top: 1px solid var(--border);
    color: var(--text-muted); font-size: 0.8rem;
    display: flex; justify-content: space-between;
  }

  @media (max-width: 640px) {
    body { padding: 1.5rem 1rem; }
    .stats { gap: 0.4rem; }
    .stat { font-size: 0.76rem; padding: 0.35rem 0.6rem; }
  }
</style>
</head>
<body>
`)

	// ── Header ──
	sb.WriteString(`<div class="plan-header">`)
	sb.WriteString("<h1>" + htmlEscape(p.Summary) + "</h1>\n")
	n := len(p.FilesToCreate) + len(p.FilesToModify)
	if n > 0 {
		sb.WriteString(fmt.Sprintf(`<div class="subtitle">%d file change(s) &mdash; %d new, %d modified</div>`,
			n, len(p.FilesToCreate), len(p.FilesToModify)))
	}
	sb.WriteString("</div>\n")

	// ── Stats bar ──
	sb.WriteString(`<div class="stats">`)
	if len(p.FilesToCreate) > 0 {
		sb.WriteString(fmt.Sprintf(`<span class="stat stat-new">+%d new</span>`, len(p.FilesToCreate)))
	}
	if len(p.FilesToModify) > 0 {
		sb.WriteString(fmt.Sprintf(`<span class="stat stat-mod">~%d modified</span>`, len(p.FilesToModify)))
	}
	if len(p.Tests) > 0 {
		sb.WriteString(fmt.Sprintf(`<span class="stat stat-test">%d tests</span>`, len(p.Tests)))
	}
	if len(p.Risks) > 0 {
		sb.WriteString(fmt.Sprintf(`<span class="stat stat-risk">%d risks</span>`, len(p.Risks)))
	}
	if len(p.PatternsToFollow) > 0 {
		sb.WriteString(fmt.Sprintf(`<span class="stat stat-pat">%d patterns</span>`, len(p.PatternsToFollow)))
	}
	sb.WriteString("</div>\n")

	// ── Architecture overview ──
	if n > 0 {
		layers := analyzeLayers(p)
		if len(layers) > 0 {
			sb.WriteString(`<div class="arch-overview">`)
			sb.WriteString(`<div class="arch-bar">`)
			for _, l := range layers {
				sb.WriteString(fmt.Sprintf(`<div class="arch-seg arch-seg-%s" style="flex:%d"></div>`, l.name, l.count))
			}
			sb.WriteString("</div>")
			sb.WriteString(`<div class="arch-legend">`)
			for _, l := range layers {
				sb.WriteString(fmt.Sprintf(`<span class="l-%s">%s &times;%d</span>`, l.name, l.label, l.count))
			}
			sb.WriteString("</div></div>\n")
		}
	}

	// ── Files to create ──
	if len(p.FilesToCreate) > 0 {
		sb.WriteString(fmt.Sprintf("<h2>Files to create <span class=\"count\">%d</span></h2>\n", len(p.FilesToCreate)))
		for _, f := range p.FilesToCreate {
			writeFileHTML(&sb, "CREATE", f.Path, f.Purpose, f.Spec, f.Follows, f.Needs, f.SideEffects)
		}
	}

	// ── Files to modify ──
	if len(p.FilesToModify) > 0 {
		sb.WriteString(fmt.Sprintf("<h2>Files to modify <span class=\"count\">%d</span></h2>\n", len(p.FilesToModify)))
		for _, f := range p.FilesToModify {
			writeFileHTML(&sb, "MODIFY", f.Path, f.Purpose, f.Spec, f.Follows, f.Needs, f.SideEffects)
		}
	}

	// ── Tests ──
	if len(p.Tests) > 0 {
		sb.WriteString(fmt.Sprintf("<h2>Tests <span class=\"count\">%d</span></h2>\n", len(p.Tests)))
		for _, t := range p.Tests {
			sb.WriteString(`<div class="test-item">`)
			sb.WriteString(`<span class="test-icon">🧪</span>`)
			sb.WriteString(`<div class="test-body">`)
			sb.WriteString(fmt.Sprintf(`<div class="test-path">%s</div>`, htmlEscape(t.File)))
			sb.WriteString(fmt.Sprintf(`<div class="test-strategy">%s</div>`, htmlEscape(t.Strategy)))
			sb.WriteString("</div></div>\n")
		}
	}

	// ── Patterns ──
	if len(p.PatternsToFollow) > 0 {
		sb.WriteString(fmt.Sprintf("<h2>Patterns <span class=\"count\">%d</span></h2>\n", len(p.PatternsToFollow)))
		sb.WriteString(`<div class="chips">`)
		for _, pt := range p.PatternsToFollow {
			sb.WriteString(fmt.Sprintf(`<span class="chip">%s</span>`, htmlEscape(pt)))
		}
		sb.WriteString("</div>\n")
	}

	// ── Risks ──
	if len(p.Risks) > 0 {
		sb.WriteString(fmt.Sprintf("<h2>Risks <span class=\"count\">%d</span></h2>\n", len(p.Risks)))
		sb.WriteString(`<ul class="item-list">`)
		for _, r := range p.Risks {
			sb.WriteString(fmt.Sprintf(`<li class="item-row"><span class="icon risk-icon">⚠</span> %s</li>`+"\n", htmlEscape(r)))
		}
		sb.WriteString("</ul>\n")
	}

	// ── Success criteria ──
	if len(p.SuccessCriteria) > 0 {
		sb.WriteString(fmt.Sprintf("<h2>Success criteria <span class=\"count\">%d</span></h2>\n", len(p.SuccessCriteria)))
		sb.WriteString(`<ul class="item-list">`)
		for _, sc := range p.SuccessCriteria {
			sb.WriteString(fmt.Sprintf(`<li class="item-row"><span class="icon check-icon">✓</span> %s</li>`+"\n", htmlEscape(sc)))
		}
		sb.WriteString("</ul>\n")
	}

	sb.WriteString("<footer><span>Generated by fabric</span></footer>\n")
	sb.WriteString("</body>\n</html>\n")
	return sb.String()
}

func writeFileHTML(sb *strings.Builder, action, path, purpose, spec, follows string, needs []string, sideEffects string) {
	sb.WriteString(`<div class="file">`)

	// Top row: action badge + path
	sb.WriteString(`<div class="file-top">`)
	if action == "CREATE" {
		sb.WriteString(`<span class="file-action action-create">new</span> `)
	} else {
		sb.WriteString(`<span class="file-action action-modify">modify</span> `)
	}
	sb.WriteString(fmt.Sprintf(`<span class="file-path">%s</span>`, htmlEscape(path)))
	sb.WriteString(`</div>`)

	// Purpose
	sb.WriteString(fmt.Sprintf(`<div class="file-purpose">%s</div>`, htmlEscape(purpose)))

	// Spec
	if spec != "" {
		sb.WriteString(fmt.Sprintf(`<div class="file-spec">%s</div>`, htmlEscape(strings.TrimSpace(spec))))
	}

	// Meta tags
	hasMeta := follows != "" || len(needs) > 0 || sideEffects != ""
	if hasMeta {
		sb.WriteString(`<div class="file-meta">`)
		if follows != "" {
			sb.WriteString(fmt.Sprintf(`<span class="tag tag-follows">follows %s</span>`, htmlEscape(follows)))
		}
		if len(needs) > 0 {
			sb.WriteString(fmt.Sprintf(`<span class="tag tag-needs">needs %s</span>`, htmlEscape(strings.Join(needs, ", "))))
		}
		if sideEffects != "" {
			sb.WriteString(fmt.Sprintf(`<span class="tag tag-side">%s</span>`, htmlEscape(sideEffects)))
		}
		sb.WriteString("</div>")
	}

	sb.WriteString("</div>\n")
}

// layerInfo holds counts for the architecture overview bar.
type layerInfo struct {
	name  string
	label string
	count int
}

func analyzeLayers(p *architect.Plan) []layerInfo {
	type layer struct {
		name     string
		label    string
		patterns []string
	}
	layers := []layer{
		{"domain", "domain", []string{"/domain/", "/aggregates/", "/events/"}},
		{"app", "application", []string{"/application/", "/command/", "/commandhandler/", "/usecase/"}},
		{"adapters", "adapters", []string{"/adapters/", "/http/", "/routes/", "/handlers/", "/repository/"}},
		{"deploy", "deployments", []string{"/deployments/", "/test/", "/integration/"}},
	}

	var counts []layerInfo
	for _, l := range layers {
		n := countInLayer(p, l.patterns)
		if n > 0 {
			counts = append(counts, layerInfo{name: l.name, label: l.label, count: n})
		}
	}
	return counts
}

func countInLayer(p *architect.Plan, patterns []string) int {
	n := 0
	for _, f := range p.FilesToCreate {
		if containsAny(f.Path, patterns) {
			n++
		}
	}
	for _, f := range p.FilesToModify {
		if containsAny(f.Path, patterns) {
			n++
		}
	}
	return n
}

func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
