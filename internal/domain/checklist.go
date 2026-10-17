package domain

import "time"

type ChecklistItem struct {
	Type       string `json:"type"` // section | question
	Text       string `json:"text"`
	HazardOnNo bool   `json:"hazard_on_no"`
	Category   string `json:"category"`
}

type ChecklistTemplate struct {
	ID         int64           `db:"id"`
	Title      string          `db:"title"`
	LocationID *int64          `db:"location_id"`
	Location   string          `db:"location"`
	Frequency  string          `db:"frequency"`
	Items      []ChecklistItem `db:"items"`
	Runs       int             `db:"runs"`
	DoneToday  bool            `db:"done_today"`
}

func (t ChecklistTemplate) Questions() int {
	n := 0
	for _, it := range t.Items {
		if it.Type != "section" {
			n++
		}
	}
	return n
}

type Answer struct {
	Text     string `json:"text"`
	Section  bool   `json:"section,omitempty"`
	Value    string `json:"value,omitempty"` // yes | no | na
	Comment  string `json:"comment,omitempty"`
	Photo    string `json:"photo,omitempty"`
	HazardID int64  `json:"hazard_id,omitempty"`
}

type Run struct {
	ID         int64     `db:"id"`
	TemplateID int64     `db:"template_id"`
	UserID     *int64    `db:"user_id"`
	LocationID *int64    `db:"location_id"`
	Title      string    `db:"title"`
	Location   string    `db:"location"`
	UserName   string    `db:"user_name"`
	Answers    []Answer  `db:"answers"`
	Issues     int       `db:"issues"`
	Total      int       `db:"total"`
	Signature  string    `db:"signature"`
	CreatedAt  time.Time `db:"created_at"`
}
