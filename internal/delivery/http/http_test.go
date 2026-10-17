package httpdelivery_test

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	httpdelivery "qauipsiz/internal/delivery/http"
	"qauipsiz/internal/domain"
	"qauipsiz/internal/infra/storage"
	"qauipsiz/internal/repository/postgres"
	"qauipsiz/internal/usecase/auth"
	"qauipsiz/internal/usecase/checklist"
	"qauipsiz/internal/usecase/dashboard"
	"qauipsiz/internal/usecase/hazard"
	"qauipsiz/internal/usecase/induction"
	"qauipsiz/internal/usecase/people"
	"qauipsiz/internal/usecase/pilot"
)

// Сквозные тесты HTTP-слоя поверх настоящих usecase и Postgres (docker compose up -d db).

var ctx = context.Background()

type nopSMS struct{}

func (nopSMS) Send(string, string) error { return nil }

type env struct {
	srv *httptest.Server
	db  *pgxpool.Pool
}

func setup(t *testing.T) env { return setupWith(t, nil) }

// setupWith — тот же стек, но с подменённым хранилищем файлов (nil — диск во временной папке).
func setupWith(t *testing.T, fs httpdelivery.FileServer) env {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://qauipsiz:qauipsiz@localhost:5433/qauipsiz?sslmode=disable"
	}
	pool, err := postgres.Connect(ctx, dsn)
	if err != nil {
		t.Skip("нет базы: ", err)
	}
	t.Cleanup(pool.Close)
	store, files := postgres.New(pool), storage.Local{Dir: t.TempDir()}
	if fs == nil {
		fs = files
	}
	hz := hazard.New(store, store, files)
	h := httpdelivery.New(httpdelivery.Services{
		Auth:       auth.New(store, nopSMS{}, "1111"),
		Hazards:    hz,
		Inductions: induction.New(store, store, files, nopSMS{}, "http://test"),
		Checklists: checklist.New(store, store, files, hz),
		People:     people.New(store, files),
		Dashboard:  dashboard.New(store),
		Pilot:      pilot.New(store),
	}, fs, httpdelivery.Options{PricePerUser: 9990, FilesOrigin: "https://s3.example.kz"})
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return env{srv: srv, db: pool}
}

// newUser создаёт организацию (или использует orgID) и сотрудника; возвращает телефон.
func (e env) newUser(t *testing.T, orgID *int64, role string) (phone string, org int64) {
	t.Helper()
	if orgID == nil {
		if err := e.db.QueryRow(ctx, `INSERT INTO orgs (name) VALUES ('Тест') RETURNING id`).Scan(&org); err != nil {
			t.Fatal(err)
		}
	} else {
		org = *orgID
	}
	phone = fmt.Sprintf("778%08d", rand.IntN(100_000_000))
	if _, err := e.db.Exec(ctx, `INSERT INTO users (org_id, phone, name, role) VALUES ($1, $2, 'Тест', $3)`, org, phone, role); err != nil {
		t.Fatal(err)
	}
	return phone, org
}

func client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e env) login(t *testing.T, phone string) *http.Client {
	t.Helper()
	c := client()
	e.post(t, c, "/login/phone", url.Values{"phone": {phone}})
	res := e.post(t, c, "/login/code", url.Values{"phone": {phone}, "code": {"1111"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("вход %s: %d", phone, res.StatusCode)
	}
	return c
}

func (e env) post(t *testing.T, c *http.Client, path string, v url.Values) *http.Response {
	t.Helper()
	res, err := c.PostForm(e.srv.URL+path, v)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func (e env) get(t *testing.T, c *http.Client, path string) (*http.Response, string) {
	t.Helper()
	res, err := c.Get(e.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestLoginAndRoles(t *testing.T) {
	e := setup(t)
	hsePhone, org := e.newUser(t, nil, domain.RoleHSE)
	workerPhone, _ := e.newUser(t, &org, domain.RoleWorker)

	if res, _ := e.get(t, client(), "/admin"); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("без входа → /login, получили %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	c := client()
	e.post(t, c, "/login/phone", url.Values{"phone": {hsePhone}})
	if res := e.post(t, c, "/login/code", url.Values{"phone": {hsePhone}, "code": {"0000"}}); res.StatusCode != http.StatusOK {
		t.Fatalf("неверный код — форма с ошибкой, получили %d", res.StatusCode)
	}
	if res, _ := e.get(t, c, "/admin"); res.StatusCode != http.StatusSeeOther {
		t.Fatal("неверный код не должен впускать")
	}

	hse := e.login(t, hsePhone)
	if res, body := e.get(t, hse, "/admin"); res.StatusCode != http.StatusOK || !strings.Contains(body, "Дашборд") {
		t.Fatalf("инженер ОТ → дашборд: %d", res.StatusCode)
	}
	if res, _ := e.get(t, hse, "/admin/settings"); res.StatusCode != http.StatusForbidden {
		t.Fatal("настройки — только админ")
	}
	worker := e.login(t, workerPhone)
	if res, _ := e.get(t, worker, "/admin"); res.StatusCode != http.StatusForbidden {
		t.Fatal("сотрудник не видит админку")
	}
	if res, _ := e.get(t, worker, "/m"); res.StatusCode != http.StatusOK {
		t.Fatal("сотрудник видит мобильную главную")
	}
	e.post(t, worker, "/logout", nil)
	if res, _ := e.get(t, worker, "/m"); res.StatusCode != http.StatusSeeOther {
		t.Fatal("после выхода сессия не действует")
	}
}

func TestCSRFAndHtmx(t *testing.T) {
	e := setup(t)
	phone, _ := e.newUser(t, nil, domain.RoleWorker)
	c := e.login(t, phone)

	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/m/hazards", strings.NewReader("category=fire&risk=low&description=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	if res, _ := c.Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatalf("POST с чужим Origin: %d", res.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, e.srv.URL+"/m/hazards", strings.NewReader("category=fire&risk=low&description=Дым"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", e.srv.URL)
	req.Header.Set("HX-Request", "true")
	res, _ := c.Do(req)
	if res.StatusCode != http.StatusOK || !regexp.MustCompile(`^/m/hazards/\d+\?created$`).MatchString(res.Header.Get("HX-Redirect")) {
		t.Fatalf("htmx-редирект: %d %q", res.StatusCode, res.Header.Get("HX-Redirect"))
	}

	res = e.post(t, c, "/m/hazards", url.Values{"category": {"fire"}, "risk": {"low"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("ошибка валидации → 400, получили %d", res.StatusCode)
	}
}

func TestOrgIsolationAndFiles(t *testing.T) {
	e := setup(t)
	phoneA, orgA := e.newUser(t, nil, domain.RoleWorker)
	adminB, _ := e.newUser(t, nil, domain.RoleAdmin)
	a, b := e.login(t, phoneA), e.login(t, adminB)

	// Сотрудник A сообщает об опасности с фото.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{"category": "electric", "risk": "high", "description": "Оголённый кабель"} {
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("photos", "a.png")
	fw.Write(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/m/hazards", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := a.Do(req)
	if err != nil || res.StatusCode != http.StatusSeeOther {
		t.Fatalf("создание с фото: %v %d", err, res.StatusCode)
	}
	cardURL := strings.TrimSuffix(res.Header.Get("Location"), "?created")

	_, card := e.get(t, a, cardURL)
	file := regexp.MustCompile(fmt.Sprintf(`/files/%d/[a-f0-9]+\.png`, orgA)).FindString(card)
	if file == "" {
		t.Fatal("в карточке нет фото")
	}
	if res, _ := e.get(t, a, file); res.StatusCode != http.StatusOK {
		t.Fatalf("свой файл: %d", res.StatusCode)
	}
	if res, _ := e.get(t, b, file); res.StatusCode != http.StatusNotFound {
		t.Fatalf("файл чужой организации: %d", res.StatusCode)
	}
	id := strings.TrimPrefix(cardURL, "/m/hazards/")
	if res, _ := e.get(t, b, "/admin/hazards/"+id); res.StatusCode != http.StatusNotFound {
		t.Fatalf("опасность чужой организации: %d", res.StatusCode)
	}
	if res := e.post(t, b, "/admin/hazards/"+id, url.Values{"risk": {"low"}, "status": {"rejected"}}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("изменение опасности чужой организации: %d", res.StatusCode)
	}
}

func TestSecurityHardening(t *testing.T) {
	e := setup(t)
	hsePhone, org := e.newUser(t, nil, domain.RoleHSE)
	c := e.login(t, hsePhone)

	// Open redirect: /lang уводит только на свой сайт.
	for ref, want := range map[string]string{
		"https://evil.example/phish":        "/",
		e.srv.URL + "/m/hazards?tab=closed": "/m/hazards?tab=closed",
		"":                                  "/",
	} {
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/lang/kk", nil)
		req.Header.Set("Referer", ref)
		res, _ := client().Do(req)
		if got := res.Header.Get("Location"); got != want {
			t.Errorf("Referer %q → %q, ожидалось %q", ref, got, want)
		}
	}

	// Заголовки безопасности: страницы нельзя встроить во фрейм, файлы — исполнить.
	res, _ := e.get(t, c, "/admin")
	for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY"} {
		if res.Header.Get(k) != want {
			t.Errorf("%s = %q", k, res.Header.Get(k))
		}
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("CSP без frame-ancestors")
	}
	res, _ = e.get(t, c, fmt.Sprintf("/files/%d/nope.png", org))
	if !strings.HasPrefix(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("файлы должны отдаваться с CSP sandbox")
	}

	// Тело больше лимита → 413, а не запись на диск.
	big := strings.NewReader("x=" + strings.Repeat("a", 2<<20))
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/admin/hazards/bulk", big)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, _ := c.Do(req); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("большая форма: %d", res.StatusCode)
	}

	// Путь к файлу шаблона нельзя подставить из формы.
	e.post(t, c, "/admin/inductions/save", url.Values{"title": {"Вводный"}, "kind": {"intro"}, "file_path": {"https://evil.example/x.pdf"}})
	var path string
	if err := e.db.QueryRow(ctx, `SELECT file_path FROM induction_templates WHERE org_id = $1`, org).Scan(&path); err != nil || path != "" {
		t.Errorf("file_path из формы попал в БД: %q %v", path, err)
	}

	// Напоминание по партии назначений чужой организации.
	_, otherOrg := e.newUser(t, nil, domain.RoleHSE)
	var tpl int64
	e.db.QueryRow(ctx, `INSERT INTO induction_templates (org_id, title, kind) VALUES ($1, 'T', 'intro') RETURNING id`, otherOrg).Scan(&tpl)
	e.db.Exec(ctx, `INSERT INTO induction_assignments (org_id, template_id, user_id, batch, due_at)
		SELECT $1, $2, id, 'foreign-batch', current_date FROM users WHERE org_id = $1`, otherOrg, tpl)
	if res := e.post(t, c, "/admin/assignments/foreign-batch/remind", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("чужая партия: %d", res.StatusCode)
	}
}

type fakeFiles struct{ calls []string }

func (f *fakeFiles) Serve(w http.ResponseWriter, _ *http.Request, orgID int64, name string) error {
	f.calls = append(f.calls, fmt.Sprintf("%d/%s", orgID, name))
	http.Redirect(w, &http.Request{}, "https://s3.example.kz/signed", http.StatusFound)
	return nil
}

func TestFilesGoThroughAccessCheck(t *testing.T) {
	fake := &fakeFiles{}
	e := setupWith(t, fake)
	phone, org := e.newUser(t, nil, domain.RoleWorker)
	c := e.login(t, phone)

	if res, _ := e.get(t, c, fmt.Sprintf("/files/%d/abc.png", org+100000)); res.StatusCode != http.StatusNotFound || len(fake.calls) != 0 {
		t.Fatalf("чужая организация: %d, хранилище вызвано %v", res.StatusCode, fake.calls)
	}
	res, _ := e.get(t, c, fmt.Sprintf("/files/%d/abc.png", org))
	if res.StatusCode != http.StatusFound || len(fake.calls) != 1 || fake.calls[0] != fmt.Sprintf("%d/abc.png", org) {
		t.Fatalf("своя организация: %d %v", res.StatusCode, fake.calls)
	}
	if res, _ := e.get(t, client(), fmt.Sprintf("/files/%d/abc.png", org)); res.StatusCode != http.StatusSeeOther || len(fake.calls) != 1 {
		t.Fatal("без входа хранилище не вызывается")
	}
	// Картинки из S3 разрешены CSP страниц.
	res, _ = e.get(t, c, "/m")
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "img-src 'self' data: blob: https://s3.example.kz") {
		t.Fatalf("CSP: %s", res.Header.Get("Content-Security-Policy"))
	}
}

func TestPhoneStoredRawShownFormatted(t *testing.T) {
	e := setup(t)
	hsePhone, org := e.newUser(t, nil, domain.RoleHSE)
	c := e.login(t, hsePhone)

	// Так поле отправляет маска на фронте.
	n := rand.IntN(10_000_000)
	masked := fmt.Sprintf("+7 776 %03d %02d %02d", n/10000, n/100%100, n%100)
	stored := fmt.Sprintf("7776%07d", n)
	if res := e.post(t, c, "/admin/people/save", url.Values{"name": {"Берик"}, "phone": {masked}, "role": {"worker"}, "active": {"1"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("сохранение: %d", res.StatusCode)
	}
	var got string
	if err := e.db.QueryRow(ctx, `SELECT phone FROM users WHERE org_id = $1 AND name = 'Берик'`, org).Scan(&got); err != nil || got != stored {
		t.Fatalf("в базе %q, ожидалось %q (%v)", got, stored, err)
	}
	_, body := e.get(t, c, "/admin/people")
	if body = html.UnescapeString(body); !strings.Contains(body, masked) || strings.Contains(body, "+"+stored) {
		t.Fatalf("в списке номер должен быть %q", masked)
	}
}
