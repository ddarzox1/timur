// Package people — сотрудники, импорт из Excel (CSV), оргструктура и настройки организации.
package people

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"qauipsiz/internal/domain"
)

type Repo interface {
	People(ctx context.Context, orgID int64, f domain.PeopleFilter) ([]domain.User, error)
	Person(ctx context.Context, orgID, id int64) (domain.User, error)
	// CreatePerson / UpdatePerson возвращают domain.ErrConflict, если телефон занят.
	CreatePerson(ctx context.Context, orgID int64, u domain.User) error
	UpdatePerson(ctx context.Context, orgID int64, u domain.User) error
	// ImportPeople — подразделения и объекты ищутся по названию и создаются; телефоны обновляются.
	// Возвращает число импортированных и номера строк, где телефон занят другой организацией.
	ImportPeople(ctx context.Context, orgID int64, rows []domain.ImportRow) (int, []int, error)
	Staff(ctx context.Context, orgID int64) ([]domain.User, error)
	Departments(ctx context.Context, orgID int64) ([]domain.Department, error)
	SaveDepartment(ctx context.Context, orgID int64, d domain.Department) error
	DeleteDepartment(ctx context.Context, orgID, id int64) error
	Locations(ctx context.Context, orgID int64) ([]domain.Location, error)
	SaveLocation(ctx context.Context, orgID int64, l domain.Location) error
	Org(ctx context.Context, id int64) (domain.Org, error)
	SaveOrg(ctx context.Context, o domain.Org) error
	ActiveUsers(ctx context.Context, orgID int64) (int, error)
}

type Storage interface {
	Save(orgID int64, f domain.Upload, onlyType string) (string, error)
}

type Service struct {
	repo  Repo
	files Storage
}

func New(repo Repo, files Storage) *Service { return &Service{repo: repo, files: files} }

func (s *Service) List(ctx context.Context, u domain.User, f domain.PeopleFilter) ([]domain.User, error) {
	f.Query = strings.TrimSpace(f.Query)
	return s.repo.People(ctx, u.OrgID, f)
}

func (s *Service) Get(ctx context.Context, u domain.User, id int64) (domain.User, error) {
	return s.repo.Person(ctx, u.OrgID, id)
}

func (s *Service) Staff(ctx context.Context, u domain.User) ([]domain.User, error) {
	return s.repo.Staff(ctx, u.OrgID)
}

// Save создаёт (ID=0) или обновляет сотрудника.
// Правила: админа назначает и редактирует только админ; себе нельзя менять роль и отключать доступ.
func (s *Service) Save(ctx context.Context, by domain.User, p domain.User) error {
	p.Name = domain.Clip(p.Name, domain.MaxTitle)
	p.Position = domain.Clip(p.Position, domain.MaxTitle)
	p.Phone = domain.NormalizePhone(p.Phone)
	if p.Name == "" || p.Phone == "" || domain.RoleLabels[p.Role] == "" {
		return domain.Invalid("Укажите ФИО, телефон (+7 7XX XXX XX XX) и роль")
	}
	if p.BirthYear != nil && (*p.BirthYear < 1900 || *p.BirthYear > 2100) {
		p.BirthYear = nil
	}
	if p.Role == domain.RoleAdmin && by.Role != domain.RoleAdmin {
		return domain.Invalid("Назначать администраторов может только администратор")
	}
	var err error
	if p.ID == 0 {
		err = s.repo.CreatePerson(ctx, by.OrgID, p)
	} else {
		old, gerr := s.repo.Person(ctx, by.OrgID, p.ID)
		if gerr != nil {
			return gerr
		}
		if old.Role == domain.RoleAdmin && by.Role != domain.RoleAdmin {
			return domain.ErrForbidden
		}
		if p.ID == by.ID && (p.Role != by.Role || !p.Active) {
			return domain.Invalid("Нельзя менять свою роль или отключать себя")
		}
		err = s.repo.UpdatePerson(ctx, by.OrgID, p)
	}
	if errors.Is(err, domain.ErrConflict) {
		return domain.Invalid("Сотрудник с таким телефоном уже есть")
	}
	return err
}

// ImportHeader — колонки шаблона импорта.
var ImportHeader = []string{"ФИО", "Телефон", "Год рождения", "Должность", "Подразделение", "Объект"}

type ImportResult struct {
	Imported int
	Errors   []string
}

const maxImportErrors = 20

// ParseCSV читает файл, сохранённый из Excel: BOM, разделитель «;» или «,», строка заголовка необязательна.
func ParseCSV(r io.Reader) ([]domain.ImportRow, []string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 5<<20))
	if err != nil {
		return nil, nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	first, _, _ := bufio.NewReader(bytes.NewReader(raw)).ReadLine()
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.FieldsPerRecord = -1
	if bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		cr.Comma = ';'
	}
	records, err := cr.ReadAll()
	if err != nil {
		return nil, nil, domain.Invalid("Не удалось прочитать CSV: " + err.Error())
	}
	var rows []domain.ImportRow
	var errs []string
	for i, rec := range records {
		line := i + 1
		if i == 0 && len(rec) > 0 && strings.Contains(strings.ToLower(rec[0]), "фио") {
			continue
		}
		for len(rec) < len(ImportHeader) {
			rec = append(rec, "")
		}
		for j := range rec {
			rec[j] = strings.TrimSpace(rec[j])
		}
		if rec[0] == "" && rec[1] == "" {
			continue
		}
		phone := domain.NormalizePhone(rec[1])
		if rec[0] == "" || phone == "" {
			errs = append(errs, fmt.Sprintf("Строка %d: нужны ФИО и телефон", line))
			continue
		}
		row := domain.ImportRow{Line: line, Name: domain.Clip(rec[0], domain.MaxTitle), Phone: phone, Position: domain.Clip(rec[3], domain.MaxTitle),
			Department: domain.Clip(rec[4], domain.MaxTitle), Location: domain.Clip(rec[5], domain.MaxTitle)}
		if y, err := strconv.Atoi(rec[2]); err == nil && y > 1900 {
			row.BirthYear = &y
		}
		rows = append(rows, row)
	}
	return rows, errs, nil
}

func (s *Service) Import(ctx context.Context, u domain.User, r io.Reader) (ImportResult, error) {
	rows, errs, err := ParseCSV(r)
	if err != nil {
		return ImportResult{}, err
	}
	n, conflicts, err := s.repo.ImportPeople(ctx, u.OrgID, rows)
	if err != nil {
		return ImportResult{}, err
	}
	for _, line := range conflicts {
		errs = append(errs, fmt.Sprintf("Строка %d: телефон занят в другой компании", line))
	}
	if len(errs) > maxImportErrors {
		errs = append(errs[:maxImportErrors], fmt.Sprintf("…и ещё %d", len(errs)-maxImportErrors))
	}
	return ImportResult{Imported: n, Errors: errs}, nil
}

// --- оргструктура ---

func (s *Service) Departments(ctx context.Context, u domain.User) ([]domain.Department, error) {
	return s.repo.Departments(ctx, u.OrgID)
}

func (s *Service) Locations(ctx context.Context, u domain.User) ([]domain.Location, error) {
	return s.repo.Locations(ctx, u.OrgID)
}

func (s *Service) Location(ctx context.Context, u domain.User, id int64) (domain.Location, error) {
	locs, err := s.repo.Locations(ctx, u.OrgID)
	if err != nil {
		return domain.Location{}, err
	}
	for _, l := range locs {
		if l.ID == id {
			return l, nil
		}
	}
	return domain.Location{}, domain.ErrNotFound
}

func (s *Service) SaveDepartment(ctx context.Context, u domain.User, d domain.Department) error {
	d.Name = domain.Clip(d.Name, domain.MaxTitle)
	if d.Name == "" {
		return domain.Invalid("Укажите название подразделения")
	}
	if d.ParentID != nil && *d.ParentID == d.ID {
		d.ParentID = nil
	}
	return s.repo.SaveDepartment(ctx, u.OrgID, d)
}

func (s *Service) DeleteDepartment(ctx context.Context, u domain.User, id int64) error {
	return s.repo.DeleteDepartment(ctx, u.OrgID, id)
}

func (s *Service) SaveLocation(ctx context.Context, u domain.User, l domain.Location, evacuation *domain.Upload) error {
	l.Name, l.Address = domain.Clip(l.Name, domain.MaxTitle), domain.Clip(l.Address, domain.MaxTitle)
	if l.Name == "" {
		return domain.Invalid("Укажите название объекта")
	}
	if l.RadiusM < 50 {
		l.RadiusM = 300
	}
	if (l.Lat == nil) != (l.Lng == nil) {
		return domain.Invalid("Укажите и широту, и долготу")
	}
	l.EvacuationPath = "" // путь задаёт только загрузка; пустой — оставить прежний файл
	if evacuation != nil {
		p, err := s.files.Save(u.OrgID, *evacuation, "")
		if err != nil {
			return err
		}
		l.EvacuationPath = p
	}
	return s.repo.SaveLocation(ctx, u.OrgID, l)
}

// --- настройки ---

type Settings struct {
	Org    domain.Org
	Active int
}

func (s *Service) Settings(ctx context.Context, u domain.User) (Settings, error) {
	o, err := s.repo.Org(ctx, u.OrgID)
	if err != nil {
		return Settings{}, err
	}
	n, err := s.repo.ActiveUsers(ctx, u.OrgID)
	return Settings{Org: o, Active: n}, err
}

func (s *Service) SaveSettings(ctx context.Context, u domain.User, o domain.Org) error {
	if u.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	o.ID = u.OrgID
	o.Name, o.BIN = domain.Clip(o.Name, domain.MaxTitle), strings.TrimSpace(o.BIN)
	if o.Name == "" {
		return domain.Invalid("Укажите название организации")
	}
	if o.BIN != "" && (len(o.BIN) != 12 || strings.Trim(o.BIN, "0123456789") != "") {
		return domain.Invalid("БИН — 12 цифр")
	}
	if o.RepeatWarnDays < 1 || o.RepeatWarnDays > 90 {
		o.RepeatWarnDays = 14
	}
	return s.repo.SaveOrg(ctx, o)
}
