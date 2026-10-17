package people

import (
	"context"
	"errors"
	"strings"
	"testing"

	"qauipsiz/internal/domain"
)

var ctx = context.Background()

type memRepo struct {
	Repo
	people   map[int64]domain.User
	imported []domain.ImportRow
	conflict []int
	savedOrg *domain.Org
}

func (m *memRepo) Person(_ context.Context, org, id int64) (domain.User, error) {
	p, ok := m.people[id]
	if !ok || p.OrgID != org {
		return p, domain.ErrNotFound
	}
	return p, nil
}
func (m *memRepo) CreatePerson(_ context.Context, org int64, u domain.User) error {
	for _, p := range m.people {
		if p.Phone == u.Phone {
			return domain.ErrConflict
		}
	}
	u.ID, u.OrgID = int64(len(m.people)+1), org
	m.people[u.ID] = u
	return nil
}
func (m *memRepo) UpdatePerson(_ context.Context, _ int64, u domain.User) error {
	m.people[u.ID] = u
	return nil
}
func (m *memRepo) ImportPeople(_ context.Context, _ int64, rows []domain.ImportRow) (int, []int, error) {
	m.imported = rows
	return len(rows) - len(m.conflict), m.conflict, nil
}
func (m *memRepo) SaveOrg(_ context.Context, o domain.Org) error { m.savedOrg = &o; return nil }

type files struct{}

func (files) Save(int64, domain.Upload, string) (string, error) { return "/files/1/plan.pdf", nil }

var (
	admin = domain.User{ID: 1, OrgID: 1, Role: domain.RoleAdmin}
	hse   = domain.User{ID: 2, OrgID: 1, Role: domain.RoleHSE}
)

func setup() (*Service, *memRepo) {
	repo := &memRepo{people: map[int64]domain.User{
		1: admin,
		2: hse,
		3: {ID: 3, OrgID: 1, Phone: "77010000003", Role: domain.RoleWorker},
	}}
	repo.people[1] = domain.User{ID: 1, OrgID: 1, Phone: "77010000001", Role: domain.RoleAdmin}
	repo.people[2] = domain.User{ID: 2, OrgID: 1, Phone: "77010000002", Role: domain.RoleHSE}
	return New(repo, files{}), repo
}

func isInvalid(err error, sub string) bool {
	var ve domain.ValidationError
	return errors.As(err, &ve) && strings.Contains(string(ve), sub)
}

func TestSaveRules(t *testing.T) {
	s, repo := setup()
	if err := s.Save(ctx, hse, domain.User{Name: "Берик", Phone: "8 702 111 22 33", Role: domain.RoleWorker, Active: true}); err != nil {
		t.Fatal(err)
	}
	if repo.people[4].Phone != "77021112233" {
		t.Fatalf("телефон нормализуется: %q", repo.people[4].Phone)
	}
	if err := s.Save(ctx, hse, domain.User{Name: "Дубль", Phone: "87010000003", Role: domain.RoleWorker}); !isInvalid(err, "уже есть") {
		t.Fatalf("дубль телефона: %v", err)
	}
	if err := s.Save(ctx, hse, domain.User{Name: "X", Phone: "123", Role: domain.RoleWorker}); !isInvalid(err, "ФИО") {
		t.Fatal("плохой телефон")
	}
	if err := s.Save(ctx, hse, domain.User{Name: "X", Phone: "77010000009", Role: domain.RoleAdmin}); !isInvalid(err, "администратор") {
		t.Fatal("инженер ОТ не может создать админа")
	}
	if err := s.Save(ctx, hse, domain.User{ID: 1, Name: "Админ", Phone: "77010000001", Role: domain.RoleWorker, Active: true}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("инженер ОТ не может редактировать админа")
	}
	if err := s.Save(ctx, admin, domain.User{ID: 1, Name: "Админ", Phone: "77010000001", Role: domain.RoleWorker, Active: true}); !isInvalid(err, "свою роль") {
		t.Fatal("нельзя понизить себя")
	}
	if err := s.Save(ctx, admin, domain.User{ID: 1, Name: "Админ", Phone: "77010000001", Role: domain.RoleAdmin}); !isInvalid(err, "отключать себя") {
		t.Fatal("нельзя отключить себя")
	}
	if err := s.Save(ctx, admin, domain.User{ID: 99, Name: "X", Phone: "77010000099", Role: domain.RoleWorker}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("сотрудник из чужой организации или несуществующий")
	}
}

func TestParseCSV(t *testing.T) {
	in := "\xef\xbb\xbfФИО;Телефон;Год рождения;Должность;Подразделение;Объект\n" +
		"Берик Оспанов;8 702 111 22 33;1991;Сварщик;Сварочный участок;ЖК Алатау\n" +
		";;;;;\n" +
		"Без телефона;;;;;\n" +
		"Асель;+7 701 000 00 04;abc\n"
	rows, errs, err := ParseCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(errs) != 1 || !strings.Contains(errs[0], "Строка 4") {
		t.Fatalf("rows=%+v errs=%v", rows, errs)
	}
	r := rows[0]
	if r.Line != 2 || r.Phone != "77021112233" || *r.BirthYear != 1991 || r.Department != "Сварочный участок" || r.Location != "ЖК Алатау" {
		t.Fatalf("строка: %+v", r)
	}
	if rows[1].BirthYear != nil || rows[1].Position != "" {
		t.Fatalf("короткая строка: %+v", rows[1])
	}

	// Запятая как разделитель, без заголовка
	rows, _, _ = ParseCSV(strings.NewReader("Ерлан,87010000002,1988,Монтажник,,\n"))
	if len(rows) != 1 || rows[0].Name != "Ерлан" {
		t.Fatalf("запятая: %+v", rows)
	}
}

func TestImportReportsConflicts(t *testing.T) {
	s, repo := setup()
	repo.conflict = []int{3}
	res, err := s.Import(ctx, hse, strings.NewReader("А;87020000001\nБ;87020000002\nВ;87020000003\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "Строка 3: телефон занят") {
		t.Fatalf("%+v", res)
	}
}

func TestSettingsAndLocation(t *testing.T) {
	s, repo := setup()
	if err := s.SaveSettings(ctx, hse, domain.Org{Name: "X"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("настройки — только админ")
	}
	if err := s.SaveSettings(ctx, admin, domain.Org{Name: "ТОО", BIN: "123"}); !isInvalid(err, "БИН") {
		t.Fatal("БИН из 12 цифр")
	}
	if err := s.SaveSettings(ctx, admin, domain.Org{Name: " ТОО ", BIN: "180340012345", RepeatWarnDays: 0}); err != nil || repo.savedOrg.RepeatWarnDays != 14 || repo.savedOrg.ID != 1 {
		t.Fatalf("сохранение: %v %+v", err, repo.savedOrg)
	}
	lat := 43.2
	if err := s.SaveLocation(ctx, hse, domain.Location{Name: "Объект", Lat: &lat}, nil); !isInvalid(err, "долготу") {
		t.Fatal("координаты парой")
	}
}

type orgRepo struct {
	memRepo
	dep  *domain.Department
	loc  *domain.Location
	locs []domain.Location
}

func (o *orgRepo) SaveDepartment(_ context.Context, _ int64, d domain.Department) error {
	o.dep = &d
	return nil
}
func (o *orgRepo) SaveLocation(_ context.Context, _ int64, l domain.Location) error {
	o.loc = &l
	return nil
}
func (o *orgRepo) Locations(context.Context, int64) ([]domain.Location, error) { return o.locs, nil }
func (o *orgRepo) Org(context.Context, int64) (domain.Org, error) {
	return domain.Org{Name: "ТОО"}, nil
}
func (o *orgRepo) ActiveUsers(context.Context, int64) (int, error) { return 7, nil }

func TestOrgStructure(t *testing.T) {
	repo := &orgRepo{locs: []domain.Location{{ID: 3, Name: "Склад"}}}
	s := New(repo, files{})
	if err := s.SaveDepartment(ctx, hse, domain.Department{Name: " "}); !isInvalid(err, "подразделения") {
		t.Fatal("пустое название подразделения")
	}
	self := int64(4)
	if err := s.SaveDepartment(ctx, hse, domain.Department{ID: 4, Name: " Склад ", ParentID: &self}); err != nil || repo.dep.ParentID != nil || repo.dep.Name != "Склад" {
		t.Fatalf("подразделение не может быть родителем самому себе: %+v", repo.dep)
	}
	lat, lng := 43.2, 76.9
	if err := s.SaveLocation(ctx, hse, domain.Location{Name: "Объект", Lat: &lat, Lng: &lng, RadiusM: 10}, &domain.Upload{}); err != nil {
		t.Fatal(err)
	}
	if repo.loc.RadiusM != 300 || repo.loc.EvacuationPath == "" {
		t.Fatalf("радиус по умолчанию и план эвакуации: %+v", repo.loc)
	}
	if err := s.SaveLocation(ctx, hse, domain.Location{Name: " "}, nil); !isInvalid(err, "объекта") {
		t.Fatal("пустое название объекта")
	}
	if l, err := s.Location(ctx, hse, 3); err != nil || l.Name != "Склад" {
		t.Fatal("объект по id")
	}
	if _, err := s.Location(ctx, hse, 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("несуществующий объект")
	}
	st, err := s.Settings(ctx, admin)
	if err != nil || st.Active != 7 || st.Org.Name != "ТОО" {
		t.Fatalf("настройки: %+v %v", st, err)
	}
}
