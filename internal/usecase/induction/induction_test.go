package induction

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"qauipsiz/internal/domain"
)

var ctx = context.Background()

type memRepo struct {
	Repo
	templates   map[int64]domain.InductionTemplate
	assigns     map[int64]domain.Assignment
	signed      map[int64]domain.Signature
	created     []int64
	reason      string
	instr       *int64
	unsigned    []domain.Notice
	users       map[int64]domain.User
	location    *int64
	basis       string
	workType    string
	docs        string
	kiosk       domain.KioskState
	expectedIIN string
	edsSigns    []domain.EDSSignature
}

func (m *memRepo) InductionTemplate(_ context.Context, org, id int64) (domain.InductionTemplate, error) {
	t, ok := m.templates[id]
	if !ok || org != 1 {
		return t, domain.ErrNotFound
	}
	return t, nil
}
func (m *memRepo) UserAssignment(_ context.Context, user, id int64) (domain.Assignment, error) {
	a, ok := m.assigns[id]
	if !ok || a.UserID != user {
		return a, domain.ErrNotFound
	}
	return a, nil
}
func (m *memRepo) SignAssignment(_ context.Context, _, id int64, s domain.Signature) (bool, error) {
	if _, done := m.signed[id]; done {
		return false, nil
	}
	m.signed[id] = s
	return true, nil
}
func (m *memRepo) UsersForAssignment(_ context.Context, _ domain.User, ids []int64) ([]domain.User, error) {
	var out []domain.User
	for _, id := range ids {
		if u, ok := m.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}
func (m *memRepo) CreateAssignments(_ context.Context, _ domain.User, _ int64, instr, location *int64,
	_, reason, basis, workType, docs string, _ time.Time, ids []int64, _ domain.InductionDoc) (int, error) {
	m.created, m.reason, m.instr, m.location = ids, reason, instr, location
	m.basis, m.workType, m.docs = basis, workType, docs
	return len(ids), nil
}
func (m *memRepo) UnsignedInBatch(_ context.Context, org int64, _ string) ([]domain.Notice, error) {
	if org != 1 {
		return nil, nil
	}
	return m.unsigned, nil
}
func (m *memRepo) BatchExists(_ context.Context, viewer domain.User, _ string) (bool, error) {
	return viewer.OrgID == 1, nil
}
func (m *memRepo) SaveInductionTemplate(_ context.Context, _ int64, t domain.InductionTemplate) error {
	m.templates[99] = t
	return nil
}
func (m *memRepo) KioskState(_ context.Context, viewer domain.User, _ string) (domain.KioskState, error) {
	if viewer.OrgID != 1 {
		return domain.KioskState{}, domain.ErrNotFound
	}
	return m.kiosk, nil
}
func (m *memRepo) CanSignBatch(_ context.Context, _ domain.User, _, iin string) error {
	if iin != m.expectedIIN {
		return domain.Invalid("ИИН не ожидается")
	}
	return nil
}
func (m *memRepo) ApplyBatchEDS(_ context.Context, _ domain.User, _ string, sig domain.EDSSignature) error {
	m.edsSigns = append(m.edsSigns, sig)
	return nil
}

type dir struct{}

func (dir) Location(_ context.Context, org, id int64) (domain.Location, error) {
	lat, lng := 43.2389, 76.8897
	return domain.Location{ID: id, Lat: &lat, Lng: &lng, RadiusM: 300}, nil
}
func (dir) UserName(_ context.Context, org, id int64) (string, error) {
	if org == 1 && id < 100 {
		return "Айгерим", nil
	}
	return "", domain.ErrNotFound
}
func (dir) DirectoryUser(_ context.Context, org, id int64) (domain.User, error) {
	if org == 1 && id < 100 {
		return domain.User{ID: id, OrgID: org, Name: "Айгерим", IIN: "870514300470"}, nil
	}
	return domain.User{}, domain.ErrNotFound
}
func (dir) Org(_ context.Context, id int64) (domain.Org, error) {
	return domain.Org{ID: id, Name: "Тест", BIN: "123"}, nil
}

type files struct{}

func (files) Save(int64, domain.Upload, string) (string, error) { return "/files/1/s.jpg", nil }
func (files) Read(int64, string) ([]byte, error)                { return []byte("pdf"), nil }

type protocol struct{}

func (protocol) Build(domain.ProtocolInput) ([]byte, string, error) {
	return []byte("%PDF-test"), "sum", nil
}

type eds struct{ result EDSResult }

func (*eds) Start(context.Context, []byte, string) (EDSOperation, error) {
	return EDSOperation{ID: "op"}, nil
}
func (e *eds) Poll(context.Context, string) (EDSResult, error) { return e.result, nil }
func (*eds) Register(context.Context, string, []byte, string) (string, int64, error) {
	return "doc", 1, nil
}
func (*eds) BuildDDC(context.Context, string, []byte) ([]byte, error) { return nil, nil }
func (e *eds) Complete(_ string, iin, name string) error {
	e.result = EDSResult{Done: true, IIN: iin, Name: name, CMS: "cms"}
	return nil
}

type sms struct{ sent []string }

func (s *sms) Send(phone, text string) error { s.sent = append(s.sent, phone+" "+text); return nil }

var (
	loc    = int64(7)
	worker = domain.User{ID: 10, OrgID: 1, Role: domain.RoleWorker, LocationID: &loc}
	hse    = domain.User{ID: 1, OrgID: 1, Role: domain.RoleHSE}
	sig    = "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n"+strings.Repeat("x", 300)))
)

func setup() (*Service, *memRepo, *sms) {
	instr := int64(1)
	repo := &memRepo{
		templates: map[int64]domain.InductionTemplate{
			1: {ID: 1, Kind: "repeat", RequireGeo: true, InstructorID: &instr},
			2: {ID: 2, Kind: "unplanned"},
			3: {ID: 3, Kind: "intro", RequireSelfie: true},
			4: {ID: 4, Kind: "primary"},
			5: {ID: 5, Kind: "target"},
		},
		assigns: map[int64]domain.Assignment{
			100: {ID: 100, TemplateID: 1, UserID: 10},
			101: {ID: 101, TemplateID: 3, UserID: 10},
		},
		signed: map[int64]domain.Signature{},
		users: map[int64]domain.User{
			10: {ID: 10, Position: "Монтажник", IIN: "900101300017"},
			11: {ID: 11, Position: "Сварщик", IIN: "900101300027"},
		},
		kiosk:       domain.KioskState{Doc: domain.InductionDoc{OrgID: 1, Batch: "b", PDFPath: "/files/1/p.pdf"}, Total: 1},
		expectedIIN: "900101300017",
	}
	s := &sms{}
	return New(repo, dir{}, files{}, s, protocol{}, &eds{}, "https://q.kz/"), repo, s
}

func f(v float64) *float64 { return &v }

func isInvalid(err error, sub string) bool {
	var ve domain.ValidationError
	return errors.As(err, &ve) && strings.Contains(string(ve), sub)
}

func TestSignValidation(t *testing.T) {
	s, _, _ := setup()
	cases := []struct {
		in   SignInput
		want string
	}{
		{SignInput{Signature: sig, Lat: f(43.2), Lng: f(76.8)}, "ознакомились"},
		{SignInput{Agree: true, Signature: "data:image/png;base64,AAAA", Lat: f(1), Lng: f(1)}, "подпись"},
		{SignInput{Agree: true, Signature: sig}, "геолокации"},
	}
	for _, c := range cases {
		if err := s.Sign(ctx, worker, 100, c.in); !isInvalid(err, c.want) {
			t.Errorf("ожидалось «%s», получили %v", c.want, err)
		}
	}
	if err := s.Sign(ctx, worker, 101, SignInput{Agree: true, Signature: sig}); !isInvalid(err, "селфи") {
		t.Errorf("селфи обязательно: %v", err)
	}
	stranger := domain.User{ID: 11, OrgID: 1}
	if err := s.Sign(ctx, stranger, 100, SignInput{Agree: true, Signature: sig}); !errors.Is(err, domain.ErrNotFound) {
		t.Error("чужое назначение подписать нельзя")
	}
}

func TestSignGeoAndOnce(t *testing.T) {
	s, repo, _ := setup()
	if err := s.Sign(ctx, worker, 100, SignInput{Agree: true, Signature: sig, Lat: f(43.2390), Lng: f(76.8899)}); err != nil {
		t.Fatal(err)
	}
	if g := repo.signed[100].GeoOK; g == nil || !*g {
		t.Fatal("рядом с объектом geo_ok=true")
	}
	if err := s.Sign(ctx, worker, 100, SignInput{Agree: true, Signature: sig, Lat: f(1), Lng: f(1)}); !isInvalid(err, "уже подписан") {
		t.Fatalf("повторная подпись: %v", err)
	}
	if err := s.Sign(ctx, worker, 101, SignInput{Agree: true, Signature: sig, Lat: f(50), Lng: f(70), Selfie: &domain.Upload{}}); err != nil {
		t.Fatal(err)
	}
	if g := repo.signed[101].GeoOK; g == nil || *g || repo.signed[101].Selfie == "" {
		t.Fatal("далеко от объекта geo_ok=false, селфи сохранено")
	}
}

func TestAssign(t *testing.T) {
	s, repo, sms := setup()
	due := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	tpl := func(id int64) *int64 { return &id }
	if _, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(1), Due: &due}); !isInvalid(err, "Выберите") {
		t.Fatal("без сотрудников — ошибка")
	}
	if _, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(2), Due: &due, UserIDs: []int64{10}}); !isInvalid(err, "причину") {
		t.Fatal("внеплановый без причины — ошибка")
	}
	if _, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(4), Due: &due, UserIDs: []int64{10}}); !isInvalid(err, "рабочее место") {
		t.Fatalf("первичный без рабочего места: %v", err)
	}
	repo.users[10] = domain.User{ID: 10, IIN: "900101300017"}
	if _, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(4), Due: &due, UserIDs: []int64{10}, LocationID: &loc}); !isInvalid(err, "профессия") {
		t.Fatalf("первичный без должности: %v", err)
	}
	repo.users[10] = domain.User{ID: 10, Position: "Монтажник", IIN: "900101300017"}
	if _, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(5), Due: &due, UserIDs: []int64{10}}); !isInvalid(err, "основание") {
		t.Fatalf("целевой без полей: %v", err)
	}
	bad := int64(500)
	if _, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(1), Due: &due, UserIDs: []int64{10}, InstructorID: &bad}); !isInvalid(err, "Неизвестный") {
		t.Fatal("инструктирующий из чужой организации")
	}
	repo.unsigned = []domain.Notice{{AssignmentID: 555, Phone: "77010000002", Title: "Работа на высоте"}}
	n, err := s.Assign(ctx, hse, AssignInput{TemplateID: tpl(1), Due: &due, UserIDs: []int64{10, 11}, NotifySMS: true})
	if err != nil || n != 2 || *repo.instr != 1 {
		t.Fatalf("Assign: n=%d err=%v (инструктирующий берётся из шаблона)", n, err)
	}
	if len(sms.sent) != 1 || !strings.Contains(sms.sent[0], "https://q.kz/m/inductions/555") {
		t.Fatalf("SMS: %v", sms.sent)
	}
	if err := s.Remind(ctx, domain.User{OrgID: 2, Role: domain.RoleHSE}, "b"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("чужая партия назначений")
	}
}

func TestEDSOperation(t *testing.T) {
	s, repo, _ := setup()
	op, err := s.StartKiosk(ctx, hse, "b")
	if err != nil || op.ID != "op" {
		t.Fatalf("start: %+v %v", op, err)
	}
	if err := s.CompleteFake(op.ID, "900101300017", "Иван Иванов"); err != nil {
		t.Fatal(err)
	}
	status, err := s.SigningStatus(ctx, hse, op.ID)
	if err != nil || !status.Done || len(repo.edsSigns) != 1 || repo.edsSigns[0].SIGEXID != "doc" {
		t.Fatalf("status=%+v signs=%+v err=%v", status, repo.edsSigns, err)
	}
}

func TestSaveTemplate(t *testing.T) {
	s, repo, _ := setup()
	if err := s.SaveTemplate(ctx, hse, domain.InductionTemplate{Title: " ", Kind: "intro"}, nil); !isInvalid(err, "название") {
		t.Fatal("пустое название")
	}
	if err := s.SaveTemplate(ctx, hse, domain.InductionTemplate{Title: "Т", Kind: "weekly"}, nil); !isInvalid(err, "вид") {
		t.Fatal("неизвестный вид")
	}
	if err := s.SaveTemplate(ctx, hse, domain.InductionTemplate{Title: " Т ", Kind: "intro"}, &domain.Upload{}); err != nil || repo.templates[99].FilePath == "" || repo.templates[99].Title != "Т" {
		t.Fatalf("сохранение: %v %+v", err, repo.templates[99])
	}
	if err := s.SaveTemplate(ctx, hse, domain.InductionTemplate{Title: "Повтор", Kind: "repeat"}, nil); err != nil || repo.templates[99].RepeatMonths != 6 {
		t.Fatalf("повторный по умолчанию через 6 месяцев: %v %+v", err, repo.templates[99])
	}
}

func (m *memRepo) UserAssignments(_ context.Context, user int64, signed bool) ([]domain.Assignment, error) {
	var out []domain.Assignment
	for _, a := range m.assigns {
		if _, ok := m.signed[a.ID]; a.UserID == user && ok == signed {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memRepo) Journal(_ context.Context, _ domain.User, kinds []string, _ domain.JournalFilter) ([]domain.Assignment, error) {
	m.reason = strings.Join(kinds, ",")
	return nil, nil
}

func TestOpenAndHistory(t *testing.T) {
	s, repo, _ := setup()
	r, err := s.Open(ctx, worker, 100)
	if err != nil || r.Location == nil || r.Template.Kind != "repeat" {
		t.Fatalf("Open: %+v %v", r, err)
	}
	if _, err := s.Open(ctx, domain.User{ID: 11, OrgID: 1}, 100); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("чужое назначение")
	}
	s.Sign(ctx, worker, 100, SignInput{Agree: true, Signature: sig, Lat: f(43.2390), Lng: f(76.8899)})
	pending, _ := s.Pending(ctx, worker)
	signed, _ := s.Signed(ctx, worker)
	if len(pending) != 1 || len(signed) != 1 {
		t.Fatalf("ожидают %d, подписано %d", len(pending), len(signed))
	}
	if all, err := s.UserHistory(ctx, hse, worker.ID); err != nil || len(all) != 2 {
		t.Fatalf("история: %d %v", len(all), err)
	}
	if _, err := s.UserHistory(ctx, hse, 500); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("история сотрудника чужой организации")
	}
	s.Journal(ctx, hse, domain.JournalFilter{})
	if repo.reason != "intro" {
		t.Fatalf("вводный журнал: %s", repo.reason)
	}
	s.Journal(ctx, hse, domain.JournalFilter{Workplace: true})
	if repo.reason != "primary,repeat,unplanned,target" {
		t.Fatalf("журнал на рабочем месте: %s", repo.reason)
	}
}
