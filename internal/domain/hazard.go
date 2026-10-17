package domain

import "time"

type Hazard struct {
	ID            int64      `db:"id"`
	OrgID         int64      `db:"org_id"`
	AuthorID      *int64     `db:"author_id"`
	LocationID    *int64     `db:"location_id"`
	Place         string     `db:"place"`
	Category      string     `db:"category"`
	Risk          string     `db:"risk"`
	Description   string     `db:"description"`
	ResponsibleID *int64     `db:"responsible_id"`
	DueAt         *time.Time `db:"due_at"`
	Status        string     `db:"status"`
	Action        string     `db:"action"`
	CreatedAt     time.Time  `db:"created_at"`
	FixedAt       *time.Time `db:"fixed_at"`
	Author        string     `db:"author"`
	Location      string     `db:"location"`
	Responsible   string     `db:"responsible"`
	Photo         string     `db:"photo"`
	Lat           *float64   `db:"lat"`
	Lng           *float64   `db:"lng"`
}

func (h Hazard) Active() bool { return h.Status == "open" || h.Status == "in_progress" }

func (h Hazard) Overdue() bool { return h.Active() && h.DueAt != nil && h.DueAt.Before(Today()) }

// VisibleTo — сотрудник видит только опасности, которые сообщил сам или которые ему назначены.
func (h Hazard) VisibleTo(u User) bool {
	return h.OrgID == u.OrgID && (u.IsStaff() || eqID(h.AuthorID, u.ID) || eqID(h.ResponsibleID, u.ID))
}

func (h Hazard) IsResponsible(u User) bool { return eqID(h.ResponsibleID, u.ID) }

func eqID(p *int64, id int64) bool { return p != nil && *p == id }

func SameID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

type Photo struct {
	Kind string `db:"kind"`
	Path string `db:"path"`
}

type Event struct {
	Text      string    `db:"text"`
	CreatedAt time.Time `db:"created_at"`
	User      string    `db:"user_name"`
}

type HazardFilter struct{ Status, Risk, Category, Location, Responsible, From, To string }
