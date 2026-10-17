package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"qauipsiz/internal/domain"
)

// Интеграционные тесты: нужен Postgres (docker compose up -d db). Без базы — пропускаются.

var ctx = context.Background()

func testStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://qauipsiz:qauipsiz@localhost:5433/qauipsiz?sslmode=disable"
	}
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Skip("нет базы: ", err)
	}
	t.Cleanup(pool.Close)
	return New(pool), pool
}

// fixture — отдельная организация на тест, чтобы тесты не мешали друг другу и демо-данным.
type fixture struct {
	org  int64
	user func(role string) int64
	loc  int64
}

func newFixture(t *testing.T, db *pgxpool.Pool) fixture {
	t.Helper()
	var f fixture
	must(t, db.QueryRow(ctx, `INSERT INTO orgs (name) VALUES ('Тест') RETURNING id`).Scan(&f.org))
	must(t, db.QueryRow(ctx, `INSERT INTO locations (org_id, name) VALUES ($1, 'Объект') RETURNING id`, f.org).Scan(&f.loc))
	f.user = func(role string) int64 {
		var id int64
		must(t, db.QueryRow(ctx, `INSERT INTO users (org_id, phone, name, role) VALUES ($1, $2, 'Тест', $3) RETURNING id`,
			f.org, randomPhone(), role).Scan(&id))
		return id
	}
	return f
}

func randomPhone() string { return fmt.Sprintf("779%08d", rand.IntN(100_000_000)) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestHazardFiltersAndIsolation(t *testing.T) {
	s, db := testStore(t)
	a, b := newFixture(t, db), newFixture(t, db)
	author := a.user(domain.RoleWorker)
	past := time.Now().AddDate(0, 0, -3)
	open, err := s.CreateHazard(ctx, domain.Hazard{OrgID: a.org, AuthorID: &author, LocationID: &a.loc, Category: "fire", Risk: "high",
		Description: "x", Status: "open", DueAt: &past}, []string{"/files/1/a.jpg"})
	must(t, err)
	_, err = s.CreateHazard(ctx, domain.Hazard{OrgID: a.org, Category: "ppe", Risk: "low", Description: "y", Status: "fixed"}, nil)
	must(t, err)
	foreign, err := s.CreateHazard(ctx, domain.Hazard{OrgID: b.org, Category: "ppe", Risk: "low", Description: "z", Status: "open"}, nil)
	must(t, err)

	for f, want := range map[domain.HazardFilter]int{
		{}:                                    2,
		{Status: "active"}:                    1,
		{Status: "overdue"}:                   1,
		{Risk: "low"}:                         1,
		{Category: "fire"}:                    1,
		{Location: fmt.Sprint(a.loc)}:         1,
		{From: "2999-01-01"}:                  0,
		{To: time.Now().Format("2006-01-02")}: 2,
	} {
		hs, err := s.Hazards(ctx, a.org, f)
		must(t, err)
		if len(hs) != want {
			t.Errorf("фильтр %+v: %d, ожидалось %d", f, len(hs), want)
		}
	}
	h, err := s.Hazard(ctx, a.org, open)
	must(t, err)
	if h.Photo != "/files/1/a.jpg" || h.Location != "Объект" {
		t.Fatalf("join'ы: %+v", h)
	}
	if ev, _ := s.HazardEvents(ctx, open); len(ev) != 1 || ev[0].Text != "Создано" {
		t.Fatalf("событие создания: %+v", ev)
	}
	if _, err := s.Hazard(ctx, a.org, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("опасность чужой организации должна быть не найдена")
	}
	resp := a.user(domain.RoleWorker)
	changed, err := s.AssignHazards(ctx, a.org, []int64{open, foreign}, resp)
	must(t, err)
	if len(changed) != 1 || changed[0] != open {
		t.Fatalf("массовое назначение затронуло чужую организацию: %v", changed)
	}
	if err := s.FixHazard(ctx, b.org, open, resp, "чужая организация", "/files/2/x.jpg"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("FixHazard из чужой организации: %v", err)
	}
	must(t, s.FixHazard(ctx, a.org, open, resp, "готово", "/files/1/b.jpg"))
	h, _ = s.Hazard(ctx, a.org, open)
	ph, _ := s.HazardPhotos(ctx, open)
	if h.Status != "fixed" || h.FixedAt == nil || len(ph) != 2 || ph[1].Kind != "after" {
		t.Fatalf("устранение: %+v %+v", h, ph)
	}
	mine, _ := s.UserHazards(ctx, a.org, resp, true)
	if len(mine) != 1 {
		t.Fatalf("закрытые опасности ответственного: %d", len(mine))
	}
}

func TestJournalAndExpiring(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	worker, hse := f.user(domain.RoleWorker), f.user(domain.RoleHSE)
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Вводный", Kind: "intro"}))
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Высота", Kind: "repeat", RepeatMonths: 6}))
	ts, err := s.InductionTemplates(ctx, f.org)
	must(t, err)
	ids := map[string]int64{}
	for _, x := range ts {
		ids[x.Kind] = x.ID
	}
	due := time.Now().AddDate(0, 0, 3)
	for _, kind := range []string{"intro", "repeat"} {
		n, err := s.CreateAssignments(ctx, f.org, ids[kind], &hse, "b-"+kind+fmt.Sprint(f.org), "", due, []int64{worker, 999999})
		must(t, err)
		if n != 1 {
			t.Fatalf("назначено %d — чужой id не должен попасть", n)
		}
	}
	pending, _ := s.UserAssignments(ctx, worker, false)
	if len(pending) != 2 {
		t.Fatalf("ожидают подписи: %d", len(pending))
	}
	sig := domain.Signature{Image: "data:image/png;base64,x"}
	for _, a := range pending {
		ok, err := s.SignAssignment(ctx, worker, a.ID, sig)
		must(t, err)
		if !ok {
			t.Fatal("первая подпись")
		}
		if ok, _ := s.SignAssignment(ctx, worker, a.ID, sig); ok {
			t.Fatal("повторная подпись не должна проходить")
		}
	}

	intro, _ := s.Journal(ctx, f.org, []string{"intro"}, domain.JournalFilter{})
	work, _ := s.Journal(ctx, f.org, domain.WorkplaceKinds, domain.JournalFilter{})
	future, _ := s.Journal(ctx, f.org, domain.WorkplaceKinds, domain.JournalFilter{From: "2999-01-01"})
	if len(intro) != 1 || intro[0].Kind != "intro" || len(work) != 1 || work[0].Kind != "repeat" || len(future) != 0 {
		t.Fatalf("журналы: вводный %d, рабочее место %d, будущее %d", len(intro), len(work), len(future))
	}

	// Повторный подписан 6 месяцев назад → истекает; после нового назначения — уже нет.
	_, err = db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now() - interval '6 months' + interval '5 days' WHERE template_id = $1`, ids["repeat"])
	must(t, err)
	exp, err := s.ExpiringRepeats(ctx, f.org, 14)
	must(t, err)
	if len(exp) != 1 || exp[0].UserID != worker {
		t.Fatalf("истекающие: %+v", exp)
	}
	if exp, _ := s.ExpiringRepeats(ctx, f.org, 2); len(exp) != 0 {
		t.Fatal("за 2 дня до срока ещё не предупреждаем")
	}
	_, err = s.CreateAssignments(ctx, f.org, ids["repeat"], &hse, "renew"+fmt.Sprint(f.org), "", due, []int64{worker})
	must(t, err)
	if exp, _ := s.ExpiringRepeats(ctx, f.org, 14); len(exp) != 0 {
		t.Fatal("после назначения нового повторного предупреждение пропадает")
	}
}

func TestPeopleConflictAndImport(t *testing.T) {
	s, db := testStore(t)
	a, b := newFixture(t, db), newFixture(t, db)
	phone := randomPhone()
	must(t, s.CreatePerson(ctx, a.org, domain.User{Name: "Ерлан", Phone: phone, Role: domain.RoleWorker, Active: true, LocationID: &b.loc}))
	if err := s.CreatePerson(ctx, a.org, domain.User{Name: "Дубль", Phone: phone, Role: domain.RoleWorker}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("дубль телефона: %v", err)
	}
	ppl, _ := s.People(ctx, a.org, domain.PeopleFilter{Query: "Ерлан"})
	if len(ppl) != 1 || ppl[0].LocationID != nil {
		t.Fatalf("объект чужой организации не должен привязываться: %+v", ppl)
	}

	n, conflicts, err := s.ImportPeople(ctx, b.org, []domain.ImportRow{
		{Line: 2, Name: "Берик", Phone: randomPhone(), Department: "Сварочный участок", Location: "Новый объект"},
		{Line: 3, Name: "Асель", Phone: randomPhone(), Department: "сварочный УЧАСТОК"},
		{Line: 4, Name: "Чужой", Phone: phone},
	})
	must(t, err)
	if n != 2 || len(conflicts) != 1 || conflicts[0] != 4 {
		t.Fatalf("импорт: %d, конфликты %v", n, conflicts)
	}
	deps, _ := s.Departments(ctx, b.org)
	if len(deps) != 1 || deps[0].People != 2 {
		t.Fatalf("подразделение создаётся один раз без учёта регистра: %+v", deps)
	}
	if got, _ := s.People(ctx, a.org, domain.PeopleFilter{Query: "Ерлан"}); len(got) != 1 || got[0].Name != "Ерлан" {
		t.Fatal("импорт другой организации не должен перезаписать сотрудника")
	}
}

func TestOTPCodes(t *testing.T) {
	s, _ := testStore(t)
	phone, now := randomPhone(), time.Now()
	must(t, s.SaveCode(ctx, phone, "h1", now.Add(-time.Second)))
	if _, err := s.ActiveCode(ctx, phone, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("истёкший код не активен")
	}
	must(t, s.SaveCode(ctx, phone, "h2", now.Add(time.Minute)))
	c, err := s.ActiveCode(ctx, phone, now)
	must(t, err)
	if c.Hash != "h2" {
		t.Fatalf("активный код: %+v", c)
	}
	must(t, s.IncAttempts(ctx, c.ID))
	if c, _ := s.ActiveCode(ctx, phone, now); c.Attempts != 1 {
		t.Fatal("попытки")
	}
	if ok, _ := s.UseCode(ctx, c.ID); !ok {
		t.Fatal("первое использование")
	}
	if ok, _ := s.UseCode(ctx, c.ID); ok {
		t.Fatal("код одноразовый")
	}
}

func TestUnsignedInBatchIsolation(t *testing.T) {
	s, db := testStore(t)
	a, b := newFixture(t, db), newFixture(t, db)
	batch := fmt.Sprintf("same-%d", a.org) // одинаковое имя партии в двух организациях
	for _, f := range []fixture{a, b} {
		must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "T", Kind: "intro"}))
		ts, _ := s.InductionTemplates(ctx, f.org)
		_, err := s.CreateAssignments(ctx, f.org, ts[0].ID, nil, batch, "", time.Now(), []int64{f.user(domain.RoleWorker)})
		must(t, err)
	}
	ns, err := s.UnsignedInBatch(ctx, a.org, batch)
	must(t, err)
	if len(ns) != 1 {
		t.Fatalf("напоминания ушли бы и в чужую организацию: %d", len(ns))
	}
}
