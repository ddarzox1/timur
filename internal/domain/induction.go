package domain

import "time"

type InductionTemplate struct {
	ID            int64  `db:"id"`
	Title         string `db:"title"`
	TitleKK       string `db:"title_kk"`
	Kind          string `db:"kind"`
	RepeatMonths  int    `db:"repeat_months"`
	InstructorID  *int64 `db:"instructor_id"`
	Instructor    string `db:"instructor"`
	Body          string `db:"body"`
	FilePath      string `db:"file_path"`
	RequireGeo    bool   `db:"require_geo"`
	RequireSelfie bool   `db:"require_selfie"`
	Assigned      int    `db:"assigned"`
}

func (t InductionTemplate) TitleIn(lang string) string { return titleIn(lang, t.Title, t.TitleKK) }

type Assignment struct {
	ID                   int64      `db:"id"`
	TemplateID           int64      `db:"template_id"`
	UserID               int64      `db:"user_id"`
	Batch                string     `db:"batch"`
	Reason               string     `db:"reason"`
	LocationID           *int64     `db:"location_id"`
	WorkLocation         string     `db:"work_location"`
	Basis                string     `db:"basis"`
	WorkType             string     `db:"work_type"`
	Docs                 string     `db:"docs"`
	AssignedAt           time.Time  `db:"assigned_at"`
	DueAt                time.Time  `db:"due_at"`
	SignedAt             *time.Time `db:"signed_at"`
	Signature            string     `db:"signature"`
	SelfiePath           string     `db:"selfie_path"`
	GeoOK                *bool      `db:"geo_ok"`
	Title                string     `db:"title"`
	TitleKK              string     `db:"title_kk"`
	Kind                 string     `db:"kind"`
	UserName             string     `db:"user_name"`
	BirthYear            *int       `db:"birth_year"`
	Position             string     `db:"position"`
	Department           string     `db:"department"`
	Instructor           string     `db:"instructor"`
	InstructorPos        string     `db:"instructor_pos"`
	UserIIN              string     `db:"user_iin"`
	SignerIIN            string     `db:"iin"`
	SignerName           string     `db:"signer_name"`
	CMS                  string     `db:"cms"`
	SIGEXSignID          *int64     `db:"sigex_sign_id"`
	InstructorSignedAt   *time.Time `db:"instructor_signed_at"`
	InstructorSignerIIN  string     `db:"instructor_signer_iin"`
	InstructorSignerName string     `db:"instructor_signer_name"`
	SIGEXID              string     `db:"sigex_id"`
}

func (a Assignment) TitleIn(lang string) string { return titleIn(lang, a.Title, a.TitleKK) }

func (a Assignment) Overdue() bool { return a.SignedAt == nil && a.DueAt.Before(Today()) }

func titleIn(lang, ru, kk string) string {
	if lang == "kk" && kk != "" {
		return kk
	}
	return ru
}

// Signature — данные подписи инструктажа.
type Signature struct {
	Image  string
	Selfie string
	Lat    *float64
	Lng    *float64
	GeoOK  *bool
}

type Batch struct {
	Batch      string    `db:"batch"`
	Title      string    `db:"title"`
	Kind       string    `db:"kind"`
	AssignedAt time.Time `db:"assigned_at"`
	DueAt      time.Time `db:"due_at"`
	Total      int       `db:"total"`
	Signed     int       `db:"signed"`
}

// InductionDoc — неизменяемый PDF-протокол одной партии назначений и его подписи в SIGEX.
type InductionDoc struct {
	OrgID              int64      `db:"org_id"`
	Batch              string     `db:"batch"`
	PDFPath            string     `db:"pdf_path"`
	SHA256             string     `db:"sha256"`
	SIGEXID            string     `db:"sigex_id"`
	AllowVisitors      bool       `db:"allow_visitors"`
	InstructorSignedAt *time.Time `db:"instructor_signed_at"`
	InstructorIIN      string     `db:"instructor_iin"`
	InstructorName     string     `db:"instructor_name"`
	InstructorCMS      string     `db:"instructor_cms"`
	InstructorSignID   *int64     `db:"instructor_sign_id"`
	CreatedAt          time.Time  `db:"created_at"`
}

type EDSSignature struct {
	IIN, Name, CMS string
	SIGEXID        string
	SignID         int64
}

type KioskState struct {
	Doc               InductionDoc
	Total, Signed     int
	InstructorIIN     string
	InstructorName    string
	InstructorPending bool
}

type ProtocolInput struct {
	Org         Org
	Template    InductionTemplate
	Batch       string
	AssignedAt  time.Time
	DueAt       time.Time
	Instructor  string
	Location    string
	Reason      string
	Basis       string
	WorkType    string
	Docs        string
	MaterialSHA string
}

// Notice — кому и о чём напомнить по SMS.
type Notice struct {
	AssignmentID int64  `db:"id"`
	Phone        string `db:"phone"`
	Title        string `db:"title"`
}

type JournalFilter struct {
	Workplace                  bool
	From, To, Department, Name string
}

type Expiring struct {
	UserID   int64     `db:"user_id"`
	UserName string    `db:"user_name"`
	Template int64     `db:"template_id"`
	Title    string    `db:"title"`
	NextDue  time.Time `db:"next_due"`
}
