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

func randomIIN() string { return fmt.Sprintf("99%010d", rand.Int64N(10_000_000_000)) }

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
	_, err = db.Exec(ctx, `UPDATE induction_assignments SET knowledge_result='passed' WHERE org_id=$1 AND batch LIKE 'b-%'`, f.org)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO induction_records (assignment_id, org_id, pdf_path, sha256, instructor_signed_at)
		SELECT id, org_id, '/files/record.pdf', 'sha', now() FROM induction_assignments WHERE org_id=$1 AND batch LIKE 'b-%'`, f.org)
	must(t, err)

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
	// Случайные ИИН: индекс входа уникален по всей базе, а в ней могут быть демо-пользователи seed.
	iinA, iinB, iinC, iinD := randomIIN(), randomIIN(), randomIIN(), randomIIN()
	must(t, s.CreatePerson(ctx, a.org, domain.User{Name: "Ерлан", Phone: phone, IIN: iinA, Role: domain.RoleWorker, Active: true, LocationID: &b.loc}, "hash"))
	if err := s.CreatePerson(ctx, a.org, domain.User{Name: "Дубль", Phone: phone, IIN: iinB, Role: domain.RoleWorker}, "hash"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("дубль телефона: %v", err)
	}
	iin := "870514300470"
	must(t, s.CreatePerson(ctx, a.org, domain.User{Name: "Посетитель", IIN: iin, Role: domain.RoleVisitor}, ""))
	if err := s.CreatePerson(ctx, a.org, domain.User{Name: "Дубль ИИН", IIN: iin, Role: domain.RoleVisitor}, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("дубль ИИН в организации: %v", err)
	}
	must(t, s.CreatePerson(ctx, b.org, domain.User{Name: "Тот же посетитель", IIN: iin, Role: domain.RoleVisitor}, ""))
	ppl, _ := s.People(ctx, domain.User{OrgID: a.org, Role: domain.RoleHSE}, domain.PeopleFilter{Query: "Ерлан"})
	if len(ppl) != 1 || ppl[0].LocationID != nil {
		t.Fatalf("объект чужой организации не должен привязываться: %+v", ppl)
	}

	n, conflicts, _, err := s.ImportPeople(ctx, b.org, []domain.ImportRow{
		{Line: 2, Name: "Берик", Phone: randomPhone(), IIN: iinC, PasswordHash: "hash", Department: "Сварочный участок", Location: "Новый объект"},
		{Line: 3, Name: "Асель", Phone: randomPhone(), IIN: iinD, PasswordHash: "hash", Department: "сварочный УЧАСТОК"},
		{Line: 4, Name: "Чужой", Phone: phone, IIN: iinA, PasswordHash: "hash"},
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
	_, err = db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now(), knowledge_result='passed' WHERE batch = $1 AND org_id = $2`, batch, f.org)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO induction_records (assignment_id, org_id, pdf_path, sha256, instructor_signed_at)
		SELECT id, org_id, '/files/record.pdf', 'sha', now() FROM induction_assignments WHERE batch=$1 AND org_id=$2`, batch, f.org)
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
		if kpi.Total != 1 || kpi.Completed != 1 {
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
	_, err = db.Exec(ctx, `UPDATE users SET name='Новое имя', position='Новая должность' WHERE id=$1`, worker)
	must(t, err)
	rows, err = s.BatchAssignments(ctx, viewer, batch)
	must(t, err)
	if rows[0].UserName == "Новое имя" || rows[0].Position == "Новая должность" {
		t.Fatalf("снимок записи изменился вместе с карточкой сотрудника: %+v", rows[0])
	}
}

func TestAssignmentRecordEDSOrder(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	worker, instructor := f.user(domain.RoleWorker), f.user(domain.RoleInstructor)
	_, err := db.Exec(ctx, `UPDATE users SET iin=CASE id WHEN $1 THEN '900101300017' ELSE '900101300027' END,
		name=CASE id WHEN $1 THEN 'Работник' ELSE 'Инструктор' END WHERE id=ANY($2)`, worker, []int64{worker, instructor})
	must(t, err)
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Персональная запись", Kind: "primary"}))
	templates, _ := s.InductionTemplates(ctx, f.org)
	batch := fmt.Sprintf("record-%d", f.org)
	viewer := domain.User{ID: instructor, OrgID: f.org, Role: domain.RoleHSE}
	_, err = s.CreateAssignments(ctx, viewer, templates[0].ID, &instructor, &f.loc, batch, "", "", "", "", time.Now(),
		[]int64{worker}, testInductionDoc(f.org, batch))
	must(t, err)
	rows, err := s.BatchAssignments(ctx, viewer, batch)
	must(t, err)
	id := rows[0].ID
	docID := fmt.Sprintf("doc-%d", f.org)

	instructorSig := domain.EDSSignature{IIN: "900101300027", Name: "Инструктор", CMS: "cms-i", EnrichedCMS: "cms-i-enriched",
		SIGEXID: docID, SignID: 2, DDCPath: "/files/ddc.pdf", DDCSHA256: "ddc-sha",
		Evidence: domain.SignatureEvidence{Certificate: "cert-i", TSP: []byte(`{"valid":true}`), OCSP: []byte(`{"status":"good"}`)}}
	if err := s.ApplyAssignmentEDS(ctx, viewer, id, true, instructorSig); err == nil {
		t.Fatal("нельзя подписать запись до фиксации результата")
	}
	must(t, s.SaveKnowledgeRecord(ctx, viewer, id, domain.KnowledgePassed, "/files/record.pdf", "sha"))
	if err := s.SaveKnowledgeRecord(ctx, viewer, id, domain.KnowledgeFailed, "/files/changed.pdf", "changed"); err == nil {
		t.Fatal("замороженный результат нельзя заменить")
	}
	if err := s.ApplyAssignmentEDS(ctx, viewer, id, true, instructorSig); err == nil {
		t.Fatal("инструктор не должен подписывать раньше работника")
	}
	wrong := domain.EDSSignature{IIN: "900101300099", Name: "Чужой", CMS: "cms-x", SIGEXID: docID, SignID: 1}
	if err := s.ApplyAssignmentEDS(ctx, viewer, id, false, wrong); err == nil {
		t.Fatal("чужой ИИН должен быть отклонён")
	}
	workerSig := domain.EDSSignature{IIN: "900101300017", Name: "Работник", CMS: "cms-w", EnrichedCMS: "cms-w-enriched",
		SIGEXID: docID, SignID: 1, Evidence: domain.SignatureEvidence{Certificate: "cert-w", TSP: []byte(`{"valid":true}`), OCSP: []byte(`{"status":"good"}`)}}
	must(t, s.ApplyAssignmentEDS(ctx, viewer, id, false, workerSig))
	must(t, s.ApplyAssignmentEDS(ctx, viewer, id, true, instructorSig))
	must(t, s.ApplyAssignmentEDS(ctx, viewer, id, false, workerSig))
	a, err := s.ScopedAssignment(ctx, viewer, id)
	must(t, err)
	if a.KnowledgeResult != domain.KnowledgePassed || a.SignedAt == nil || a.InstructorSignedAt == nil || a.SIGEXID != docID ||
		a.RecordDDCPath != instructorSig.DDCPath || a.RecordDDCSHA256 != instructorSig.DDCSHA256 {
		t.Fatalf("неполная подписанная запись: %+v", a)
	}
	var archived int
	must(t, db.QueryRow(ctx, `SELECT count(*) FROM induction_signatures WHERE assignment_id=$1
		AND cms_enriched <> '' AND certificate <> '' AND tsp->>'valid'='true' AND ocsp->>'status'='good'`, id).Scan(&archived))
	if archived != 2 {
		t.Fatalf("архив доказательств подписи неполон: %d", archived)
	}
}

func TestSigningOperationPersistenceAndLease(t *testing.T) {
	s, db := testStore(t)
	f := newFixture(t, db)
	worker, instructor := f.user(domain.RoleWorker), f.user(domain.RoleInstructor)
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Операция ЭЦП", Kind: "intro"}))
	templates, _ := s.InductionTemplates(ctx, f.org)
	batch := fmt.Sprintf("op-%d", f.org)
	viewer := domain.User{ID: instructor, OrgID: f.org, Role: domain.RoleHSE}
	_, err := s.CreateAssignments(ctx, viewer, templates[0].ID, &instructor, nil, batch, "", "", "", "", time.Now(),
		[]int64{worker}, testInductionDoc(f.org, batch))
	must(t, err)
	rows, err := s.BatchAssignments(ctx, viewer, batch)
	must(t, err)
	op := domain.SigningOperationRecord{ID: fmt.Sprintf("operation-%d", f.org), OrgID: f.org, ViewerID: instructor,
		ViewerRole: domain.RoleHSE, AssignmentID: rows[0].ID, Batch: batch, ExpectedIIN: "900101300017",
		ExpiresAt: time.Now().Add(10 * time.Minute), Stage: domain.SigningPending, SignURL: "https://sigex.example/sign"}
	must(t, s.CreateSigningOperation(ctx, op))

	claimed, ok, err := s.ClaimSigningOperation(ctx, f.org, op.ID)
	must(t, err)
	if !ok || claimed.LeaseUntil == nil || claimed.SignURL != op.SignURL {
		t.Fatalf("операция не захвачена: %+v, claimed=%v", claimed, ok)
	}
	if _, ok, err := s.ClaimSigningOperation(ctx, f.org, op.ID); err != nil || ok {
		t.Fatalf("параллельный claim должен быть отклонён: claimed=%v err=%v", ok, err)
	}
	must(t, s.ReleaseSigningOperation(ctx, f.org, op.ID))
	claimed, ok, err = s.ClaimSigningOperation(ctx, f.org, op.ID)
	must(t, err)
	if !ok {
		t.Fatal("после release операция должна снова захватываться")
	}
	claimed.Stage, claimed.CMS, claimed.SignerIIN = domain.SigningSigned, "cms", "900101300017"
	claimed.DDCPath, claimed.DDCSHA256 = "/files/ddc.pdf", "ddc-sha"
	must(t, s.SaveSigningOperation(ctx, claimed))
	stored, err := s.SigningOperation(ctx, f.org, op.ID)
	must(t, err)
	if stored.Stage != domain.SigningSigned || stored.CMS != "cms" || stored.LeaseUntil != nil ||
		stored.DDCPath != claimed.DDCPath || stored.DDCSHA256 != claimed.DDCSHA256 {
		t.Fatalf("стадия операции не сохранена: %+v", stored)
	}
	if _, err := s.SigningOperation(ctx, f.org+1, op.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("операция утекла в другую организацию: %v", err)
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

func TestAdmitAssignment(t *testing.T) {
	s, db := testStore(t)
	f, other := newFixture(t, db), newFixture(t, db)
	worker, hseID := f.user(domain.RoleWorker), f.user(domain.RoleHSE)
	viewer := domain.User{ID: hseID, OrgID: f.org, Role: domain.RoleHSE}
	must(t, s.SaveInductionTemplate(ctx, f.org, domain.InductionTemplate{Title: "Первичный", Kind: "primary"}))
	ts, _ := s.InductionTemplates(ctx, f.org)
	batch := fmt.Sprintf("admit-%d", f.org)
	_, err := s.CreateAssignments(ctx, viewer, ts[0].ID, &hseID, nil, batch, "", "", "", "", time.Now(), []int64{worker}, testInductionDoc(f.org, batch))
	must(t, err)
	rows, _ := s.BatchAssignments(ctx, viewer, batch)
	id := rows[0].ID
	d1, d2 := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	in := domain.AdmitInput{Shifts: 3, From: &d1, To: &d2}

	if err := s.AdmitAssignment(ctx, viewer, id, in, "Инженер"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("допуск до подписи: %v", err)
	}
	_, err = db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now(), knowledge_result='passed' WHERE id = $1`, id)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO induction_records (assignment_id, org_id, pdf_path, sha256, instructor_signed_at)
		VALUES ($1,$2,'/files/record.pdf','sha',now())`, id, f.org)
	must(t, err)
	stranger := domain.User{ID: other.user(domain.RoleHSE), OrgID: other.org, Role: domain.RoleHSE}
	if err := s.AdmitAssignment(ctx, stranger, id, in, "Чужой"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("чужая организация: %v", err)
	}
	head := domain.User{ID: f.user(domain.RoleSectionHead), OrgID: f.org, Role: domain.RoleSectionHead}
	if err := s.AdmitAssignment(ctx, head, id, in, "Без подразделения"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("руководитель без области: %v", err)
	}
	must(t, s.AdmitAssignment(ctx, viewer, id, in, "Инженер"))
	if err := s.AdmitAssignment(ctx, viewer, id, in, "Инженер"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("повторный допуск: %v", err)
	}
	rows, _ = s.BatchAssignments(ctx, viewer, batch)
	r := rows[0]
	if r.AdmittedAt == nil || r.AdmittedByName != "Инженер" || r.InternshipShifts == nil || *r.InternshipShifts != 3 ||
		r.InternshipFrom == nil || !r.InternshipFrom.Equal(d1) {
		t.Fatalf("допуск не сохранён: %+v", r)
	}
}
