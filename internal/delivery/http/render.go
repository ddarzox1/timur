package httpdelivery

import (
	"errors"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"path"
	"strings"
	"time"

	"qauipsiz/internal/domain"
	"qauipsiz/web"
)

var pages = map[string]*template.Template{}

var funcs = template.FuncMap{
	"t":        func(s string) string { return s }, // переопределяется на запрос в renderBlock
	"date":     fmtDate,
	"datetime": fmtDateTime,
	"risk":     func(s string) string { return domain.RiskLabels[s] },
	"status":   func(s string) string { return domain.StatusLabels[s] },
	"kind":     func(s string) string { return domain.KindLabels[s] },
	"cat":      func(k string) string { c, _ := domain.CategoryByKey(k); return c.Label },
	"catIcon":  func(k string) string { c, _ := domain.CategoryByKey(k); return c.Icon },
	"role":     func(s string) string { return domain.RoleLabels[s] },
	"freq":     func(s string) string { return domain.FreqLabels[s] },
	"pct": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return int(math.Round(float64(a) * 100 / float64(b)))
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"deref": func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	},
	// safeURL — только для подписей data:image/png, проверенных domain.ValidSignature при сохранении.
	"safeURL":   func(s string) template.URL { return template.URL(s) },
	"overdue":   func(d time.Time) bool { return d.Before(domain.Today()) },
	"cats":      func() []domain.Category { return domain.Categories },
	"kinds":     func() []string { return domain.KindOrder },
	"hasSuffix": strings.HasSuffix,
	"ints":      func(xs ...int) []int { return xs },
	"strs":      func(xs ...string) []string { return xs },
}

func init() {
	files, err := fs.Glob(web.FS, "templates/pages/*.html")
	if err != nil || len(files) == 0 {
		panic("нет шаблонов")
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		pages[name] = template.Must(template.New(name).Funcs(funcs).ParseFS(web.FS, "templates/layouts/*.html", f))
	}
}

func render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) error {
	return renderBlock(w, r, page, "page", data)
}

// renderBlock рисует блок страницы. Оригинальный шаблон не исполняется — только клон,
// иначе Clone запрещён; на клоне функция t знает язык запроса.
func renderBlock(w http.ResponseWriter, r *http.Request, page, block string, data map[string]any) error {
	base, ok := pages[page]
	if !ok {
		return errors.New("нет шаблона " + page)
	}
	t, err := base.Clone()
	if err != nil {
		return err
	}
	lang := langOf(r)
	t.Funcs(template.FuncMap{"t": func(s string) string { return tr(lang, s) }})
	if data == nil {
		data = map[string]any{}
	}
	data["Lang"] = lang
	data["Path"] = r.URL.Path
	if _, ok := data["Me"]; !ok {
		data["Me"] = userFrom(r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.ExecuteTemplate(w, block, data)
}

func fmtDate(v any) string { return fmtTime(v, "02.01.2006") }

func fmtDateTime(v any) string { return fmtTime(v, "02.01.2006 15:04") }

func fmtTime(v any, layout string) string {
	switch d := v.(type) {
	case time.Time:
		return d.In(domain.KZ).Format(layout)
	case *time.Time:
		if d != nil {
			return d.In(domain.KZ).Format(layout)
		}
	}
	return "—"
}
