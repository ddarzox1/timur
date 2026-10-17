package domain

import "time"

const (
	RoleWorker = "worker"
	RoleHSE    = "hse"
	RoleAdmin  = "admin"
)

// Теги db используются репозиторием для сканирования строк; на сам домен не влияют.

type User struct {
	ID           int64  `db:"id"`
	OrgID        int64  `db:"org_id"`
	Phone        string `db:"phone"`
	Name         string `db:"name"`
	BirthYear    *int   `db:"birth_year"`
	Position     string `db:"position"`
	DepartmentID *int64 `db:"department_id"`
	LocationID   *int64 `db:"location_id"`
	Role         string `db:"role"`
	Active       bool   `db:"active"`
	OrgName      string `db:"org_name"`
	Department   string `db:"department"`
	Location     string `db:"location"`
	Overdue      bool   `db:"overdue"`
}

// IsStaff — инженер ОТ или админ: видит всю организацию.
func (u User) IsStaff() bool { return u.Role == RoleHSE || u.Role == RoleAdmin }

type Org struct {
	ID             int64      `db:"id"`
	Name           string     `db:"name"`
	BIN            string     `db:"bin"`
	LastInjuryAt   *time.Time `db:"last_injury_at"`
	RepeatWarnDays int        `db:"repeat_warn_days"`
	CreatedAt      time.Time  `db:"created_at"`
}

// DaysWithoutInjury считает от последней травмы, а если её не было — от подключения.
func (o Org) DaysWithoutInjury(today time.Time) int {
	from := o.CreatedAt
	if o.LastInjuryAt != nil {
		from = *o.LastInjuryAt
	}
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	return int(today.Sub(from).Hours() / 24)
}

type Department struct {
	ID       int64  `db:"id"`
	ParentID *int64 `db:"parent_id"`
	Name     string `db:"name"`
	Parent   string `db:"parent"`
	People   int    `db:"people"`
}

type Location struct {
	ID             int64    `db:"id"`
	Name           string   `db:"name"`
	Address        string   `db:"address"`
	Lat            *float64 `db:"lat"`
	Lng            *float64 `db:"lng"`
	RadiusM        int      `db:"radius_m"`
	ResponsibleID  *int64   `db:"responsible_id"`
	Responsible    string   `db:"responsible"`
	EvacuationPath string   `db:"evacuation_path"`
}

// Contains — точка внутри радиуса объекта. ok=false, если у объекта нет координат.
func (l Location) Contains(lat, lng float64) (inside, ok bool) {
	if l.Lat == nil || l.Lng == nil {
		return false, false
	}
	return DistanceM(lat, lng, *l.Lat, *l.Lng) <= float64(l.RadiusM), true
}

type PeopleFilter struct{ Query, Department, Location string }

type ImportRow struct {
	Line       int
	Name       string
	Phone      string
	BirthYear  *int
	Position   string
	Department string
	Location   string
}
