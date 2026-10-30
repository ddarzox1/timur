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

func testInductionDoc(org int64, batch string) domain.InductionDoc {
	return domain.InductionDoc{OrgID: org, Batch: batch, PDFPath: "/files/test/protocol.pdf", SHA256: "test"}
}

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
	viewer := domain.User{ID: hse, OrgID: f.org, Role: domain.RoleHSE}
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
		batch := "b-" + kind + fmt.Sprint(f.org)
		n, err := s.CreateAssignments(ctx, viewer, ids[kind], &hse, nil, batch, "", "", "", "", due, []int64{worker, 999999}, testInductionDoc(f.org, batch))
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

	intro, _ := s.Journal(ctx, viewer, []string{"intro"}, domain.JournalFilter{})
	work, _ := s.Journal(ctx, viewer, domain.WorkplaceKinds, domain.JournalFilter{})
	future, _ := s.Journal(ctx, viewer, domain.WorkplaceKinds, domain.JournalFilter{From: "2999-01-01"})
	if len(intro) != 1 || intro[0].Kind != "intro" || len(work) != 1 || work[0].Kind != "repeat" || len(future) != 0 {
		t.Fatalf("журналы: вводный %d, рабочее место %d, будущее %d", len(intro), len(work), len(future))
	}

	// Повторный подписан 6 месяцев назад → истекает; после нового назначения — уже нет.
	_, err = db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now() - interval '6 months' + interval '5 days' WHERE template_id = $1`, ids["repeat"])
	must(t, err)
	exp, err := s.ExpiringRepeats(ctx, viewer, 14)
	must(t, err)
	if len(exp) != 1 || exp[0].UserID != worker {
		t.Fatalf("истекающие: %+v", exp)
	}
	if exp, _ := s.ExpiringRepeats(ctx, viewer, 2); len(exp) != 0 {
		t.Fatal("за 2 дня до срока ещё не предупреждаем")
	}
	renewBatch := "renew" + fmt.Sprint(f.org)
	_, err = s.CreateAssignments(ctx, viewer, ids["repeat"], &hse, nil, renewBatch, "", "", "", "", due, []int64{worker}, testInductionDoc(f.org, renewBatch))
	must(t, err)
	if exp, _ := s.ExpiringRepeats(ctx, viewer, 14); len(exp) != 0 {
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
	iin := "870514300470"
	must(t, s.CreatePerson(ctx, a.org, domain.User{Name: "Посетитель", IIN: iin, Role: domain.RoleVisitor}))
	if err := s.CreatePerson(ctx, a.org, domain.User{Name: "Дубль ИИН", IIN: iin, Role: domain.RoleVisitor}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("дубль ИИН в организации: %v", err)
	}
	must(t, s.CreatePerson(ctx, b.org, domain.User{Name: "Тот же ИИН", IIN: iin, Role: domain.RoleVisitor}))
	ppl, _ := s.People(ctx, domain.User{OrgID: a.org, Role: domain.RoleHSE}, domain.PeopleFilter{Query: "Ерлан"})
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
	deps, _ := s.Departments(ctx, domain.User{OrgID: b.org, Role: domain.RoleHSE})
	if len(deps) != 1 || deps[0].People != 2 {
		t.Fatalf("подразделение создаётся один раз без учёта регистра: %+v", deps)
	}
	if got, _ := s.People(ctx, domain.User{OrgID: a.org, Role: domain.RoleHSE}, domain.PeopleFilter{Query: "Ерлан"}); len(got) != 1 || got[0].Name != "Ерлан" {
		t.Fatal("импорт другой организации не должен перезаписать сотрудника")
	}
}

func TestRoleScopes(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	var root, child, other int64
	must(t, db.QueryRow(ctx, `INSERT INTO departments (org_id, name) VALUES ($1, 'Цех') RETURNING id`, f.org).Scan(&root))
	must(t, db.QueryRow(ctx, `INSERT INTO departments (org_id, parent_id, name) VALUES ($1, $2, 'Участок') RETURNING id`, f.org, root).Scan(&child))
	must(t, db.QueryRow(ctx, `INSERT INTO departments (org_id, name) VALUES ($1, 'Другой цех') RETURNING id`, f.org).Scan(&other))
	add := func(role string, dep *int64) int64 {
		var id int64
		must(t, db.QueryRow(ctx, `INSERT INTO users (org_id, phone, name, role, department_id) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			f.org, randomPhone(), role+fmt.Sprint(rand.IntN(1000)), role, dep).Scan(&id))
		return id
	}
	sectionID := add(domain.RoleSectionHead, &root)
	shopID := add(domain.RoleShopHead, &root)
	instructorID := add(domain.RoleInstructor, &child)
	rootWorker := add(domain.RoleWorker, &root)
	childWorker := add(domain.RoleWorker, &child)
	otherWorker := add(domain.RoleWorker, &other)
	hseID := add(domain.RoleHSE, nil)

	section := domain.User{ID: sectionID, OrgID: f.org, Role: domain.RoleSectionHead, DepartmentID: &root}
	shop := domain.User{ID: shopID, OrgID: f.org, Role: domain.RoleShopHead, DepartmentID: &root}
	instructor := domain.User{ID: instructorID, OrgID: f.org, Role: domain.RoleInstructor, DepartmentID: &child}
	hse := domain.User{ID: hseID, OrgID: f.org, Role: domain.RoleHSE}
	for name, tc := range map[string]struct {
		viewer domain.User
		want   int
	}{"section": {section, 3}, "shop": {shop, 5}, "instructor": {instructor, 2}, "hse": {hse, 7}} {
		got, err := s.People(ctx, tc.viewer, domain.PeopleFilter{})
		must(t, err)
		if len(got) != tc.want {
			t.Errorf("%s sees %d people, want %d", name, len(got), tc.want)
		}
	}
	if _, err := s.Person(ctx, section, otherWorker); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("начальник участка не должен видеть чужое подразделение")
	}

	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "T", Kind: "intro"}))
	templates, _ := s.InductionTemplates(ctx, f.org)
	batch := fmt.Sprintf("scope-%d", f.org)
	_, err := s.CreateAssignments(ctx, hse, templates[0].ID, &instructorID, nil, batch, "", "", "", "", time.Now().AddDate(0, 0, 1), []int64{rootWorker, childWorker, otherWorker}, testInductionDoc(f.org, batch))
	must(t, err)
	_, err = db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now() WHERE batch = $1 AND org_id = $2`, batch, f.org)
	must(t, err)
	for name, tc := range map[string]struct {
		viewer domain.User
		want   int
	}{"section": {section, 1}, "shop": {shop, 2}, "instructor": {instructor, 3}, "hse": {hse, 3}} {
		rows, err := s.Journal(ctx, tc.viewer, []string{"intro"}, domain.JournalFilter{})
		must(t, err)
		if len(rows) != tc.want {
			t.Errorf("%s journal rows = %d, want %d", name, len(rows), tc.want)
		}
		batches, err := s.Batches(ctx, tc.viewer)
		must(t, err)
		if len(batches) != 1 || batches[0].Total != tc.want {
			t.Errorf("%s batches = %+v, want total %d", name, batches, tc.want)
		}
		kpi, err := s.InstructionKPI(ctx, tc.viewer)
		must(t, err)
		if kpi.Total != 1 || kpi.WaitingInstructor != 1 {
			t.Errorf("%s dashboard KPI = %+v", name, kpi)
		}
		deps, err := s.InstructionDepartments(ctx, tc.viewer)
		must(t, err)
		total := 0
		for _, d := range deps {
			total += d.A
		}
		if total != tc.want {
			t.Errorf("%s dashboard department assignments = %d, want %d", name, total, tc.want)
		}
	}
}

func TestAssignmentFieldsPersistence(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	worker := f.user(domain.RoleWorker)
	hseID := f.user(domain.RoleHSE)
	_, err := db.Exec(ctx, `UPDATE users SET position = 'Электромонтажник' WHERE id = $1`, worker)
	must(t, err)
	viewer := domain.User{ID: hseID, OrgID: f.org, Role: domain.RoleHSE}
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Огневые работы", Kind: "target"}))
	templates, _ := s.InductionTemplates(ctx, f.org)
	batch := fmt.Sprintf("fields-%d", f.org)
	n, err := s.CreateAssignments(ctx, viewer, templates[0].ID, &hseID, &f.loc, batch, "", "Наряд №7", "Сварка", "ППР-12", time.Now(), []int64{worker}, testInductionDoc(f.org, batch))
	must(t, err)
	if n != 1 {
		t.Fatalf("создано назначений: %d", n)
	}
	rows, err := s.BatchAssignments(ctx, viewer, batch)
	must(t, err)
	if len(rows) != 1 || rows[0].LocationID == nil || *rows[0].LocationID != f.loc || rows[0].WorkLocation != "Объект" ||
		rows[0].Basis != "Наряд №7" || rows[0].WorkType != "Сварка" || rows[0].Docs != "ППР-12" {
		t.Fatalf("поля проведения: %+v", rows)
	}
	users, err := s.UsersForAssignment(ctx, viewer, []int64{worker})
	must(t, err)
	if len(users) != 1 || users[0].Position != "Электромонтажник" {
		t.Fatalf("данные для валидации: %+v", users)
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
		_, err := s.CreateAssignments(ctx, domain.User{OrgID: f.org, Role: domain.RoleHSE}, ts[0].ID, nil, nil, batch, "", "", "", "", time.Now(), []int64{f.user(domain.RoleWorker)}, testInductionDoc(f.org, batch))
		must(t, err)
	}
	ns, err := s.UnsignedInBatch(ctx, a.org, batch)
	must(t, err)
	if len(ns) != 1 {
		t.Fatalf("напоминания ушли бы и в чужую организацию: %d", len(ns))
	}
}

func TestBatchEDSOrder(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	worker, instructor := f.user(domain.RoleWorker), f.user(domain.RoleInstructor)
	_, err := db.Exec(ctx, `UPDATE users SET iin=CASE id WHEN $1 THEN '900101300017' ELSE '900101300027' END WHERE id=ANY($2)`,
		worker, []int64{worker, instructor})
	must(t, err)
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "ЭЦП", Kind: "intro"}))
	templates, _ := s.InductionTemplates(ctx, f.org)
	batch := fmt.Sprintf("eds-%d", f.org)
	viewer := domain.User{ID: instructor, OrgID: f.org, Role: domain.RoleHSE}
	_, err = s.CreateAssignments(ctx, viewer, templates[0].ID, &instructor, nil, batch, "", "", "", "", time.Now(),
		[]int64{worker}, testInductionDoc(f.org, batch))
	must(t, err)

	tooEarly := domain.EDSSignature{IIN: "900101300027", Name: "Инструктор", CMS: "cms-i", SIGEXID: "doc", SignID: 1}
	if err := s.ApplyBatchEDS(ctx, viewer, batch, tooEarly); err == nil {
		t.Fatal("инструктор не должен подписывать раньше участника")
	}
	workerSig := domain.EDSSignature{IIN: "900101300017", Name: "Работник", CMS: "cms-w", SIGEXID: "doc", SignID: 1}
	must(t, s.CanSignBatch(ctx, viewer, batch, workerSig.IIN))
	must(t, s.ApplyBatchEDS(ctx, viewer, batch, workerSig))
	must(t, s.CanSignBatch(ctx, viewer, batch, tooEarly.IIN))
	tooEarly.SignID = 2
	must(t, s.ApplyBatchEDS(ctx, viewer, batch, tooEarly))
	state, err := s.KioskState(ctx, viewer, batch)
	must(t, err)
	if state.Signed != 1 || state.Total != 1 || state.Doc.InstructorSignedAt == nil || state.Doc.SIGEXID != "doc" {
		t.Fatalf("неверное состояние ЭЦП: %+v", state)
	}
}

func TestBatchEDSVisitorPermission(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	worker, instructor := f.user(domain.RoleWorker), f.user(domain.RoleInstructor)
	_, err := db.Exec(ctx, `UPDATE users SET iin=CASE id WHEN $1 THEN '900101300017' ELSE '900101300027' END WHERE id=ANY($2)`,
		worker, []int64{worker, instructor})
	must(t, err)
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Посетители", Kind: "intro"}))
	templates, _ := s.InductionTemplates(ctx, f.org)
	viewer := domain.User{ID: instructor, OrgID: f.org, Role: domain.RoleHSE}

	allowed := fmt.Sprintf("visitor-ok-%d", f.org)
	doc := testInductionDoc(f.org, allowed)
	doc.AllowVisitors = true
	_, err = s.CreateAssignments(ctx, viewer, templates[0].ID, &instructor, nil, allowed, "", "", "", "", time.Now(), []int64{worker}, doc)
	must(t, err)
	visitorSig := domain.EDSSignature{IIN: "900101300037", Name: "Гость Сериков", CMS: "cms-v", SIGEXID: "doc-v", SignID: 7}
	must(t, s.CanSignBatch(ctx, viewer, allowed, visitorSig.IIN))
	must(t, s.ApplyBatchEDS(ctx, viewer, allowed, visitorSig))
	state, err := s.KioskState(ctx, viewer, allowed)
	must(t, err)
	if state.Total != 2 || state.Signed != 1 {
		t.Fatalf("посетитель должен добавить подписанную строку: %+v", state)
	}
	var role string
	var active bool
	must(t, db.QueryRow(ctx, `SELECT role, active FROM users WHERE org_id=$1 AND iin=$2`, f.org, visitorSig.IIN).Scan(&role, &active))
	if role != domain.RoleVisitor || active {
		t.Fatalf("посетитель: role=%s active=%v", role, active)
	}

	forbidden := fmt.Sprintf("visitor-no-%d", f.org)
	_, err = s.CreateAssignments(ctx, viewer, templates[0].ID, &instructor, nil, forbidden, "", "", "", "", time.Now(), []int64{worker}, testInductionDoc(f.org, forbidden))
	must(t, err)
	stranger := domain.EDSSignature{IIN: "900101300047", Name: "Чужой", CMS: "cms-x", SIGEXID: "doc-x", SignID: 8}
	if err := s.CanSignBatch(ctx, viewer, forbidden, stranger.IIN); err == nil {
		t.Fatal("посторонний ИИН должен быть отклонён, если посетители запрещены")
	}
}
