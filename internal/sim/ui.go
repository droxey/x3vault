package sim

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/files" && r.URL.Path != "/view" {
		http.NotFound(w, r)
		return
	}

	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}

	if r.URL.Path == "/view" {
		s.renderView(w, dir)
		return
	}

	entries, err := s.Store.List(dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<title>x3sim</title>`)
	b.WriteString(`<style>
body{font:14px/1.4 ui-monospace,Menlo,Consolas,monospace;background:#111;color:#ddd;margin:24px}
a{color:#9cf} h1{font-size:16px} .muted{color:#888}
table{border-collapse:collapse} td{padding:2px 16px 2px 0}
</style></head><body>`)
	fmt.Fprintf(&b, `<h1>x3sim — fake X3 SD</h1><p class="muted">Witch file-transfer stand-in. Point <code>device.base_url</code> here.</p>`)
	fmt.Fprintf(&b, `<p>path: <code>%s</code>`, html.EscapeString(dir))
	if dir != "/" {
		fmt.Fprintf(&b, ` · <a href="/files?path=%s">parent</a>`, html.EscapeString(parentOf(dir)))
	}
	b.WriteString(`</p><table>`)
	for _, e := range entries {
		child := joinURLPath(dir, e.Name)
		if e.IsDirectory {
			fmt.Fprintf(&b, `<tr><td>dir</td><td><a href="/files?path=%s">%s/</a></td><td></td></tr>`,
				html.EscapeString(child), html.EscapeString(e.Name))
			continue
		}
		kind := "file"
		href := "/download?path=" + child
		if isMarkdown(e.Name) {
			kind = "md"
			href = "/view?path=" + child
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td><a href="%s">%s</a></td><td>%d</td></tr>`,
			kind, html.EscapeString(href), html.EscapeString(e.Name), e.Size)
	}
	b.WriteString(`</table></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) renderView(w http.ResponseWriter, p string) {
	data, ok := s.Store.ReadFile(p)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	parent := parentOf(p)
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(p))
	b.WriteString(`</title><style>
body{font:14px/1.45 ui-monospace,Menlo,Consolas,monospace;background:#111;color:#ddd;margin:24px}
a{color:#9cf} pre{white-space:pre-wrap}
</style></head><body>`)
	fmt.Fprintf(&b, `<p><a href="/files?path=%s">back</a> · <code>%s</code></p>`,
		html.EscapeString(parent), html.EscapeString(p))
	b.WriteString(`<pre>`)
	b.WriteString(html.EscapeString(string(data)))
	b.WriteString(`</pre></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
