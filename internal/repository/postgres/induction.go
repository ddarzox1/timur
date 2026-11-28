package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"qauipsiz/internal/domain"
)

const assignmentSelect = `SELECT a.id, a.template_id, a.user_id, a.batch, a.reason, a.location_id, coalesce(al.name, '') AS work_location,
	a.basis, a.work_type, a.docs, a.assigned_at, a.due_at, a.signed_at, a.signature,
	a.selfie_path, a.geo_ok, t.title, t.title_kk, t.kind,
	a.worker_name_snapshot AS user_name, a.worker_birth_year_snapshot AS birth_year,
	a.worker_position_snapshot AS position,
	a.worker_department_snapshot AS department, a.instructor_name_snapshot AS instructor,
	a.instructor_position_snapshot AS instructor_pos, coalesce(i.iin, '') AS instructor_iin,
	a.knowledge_result, coalesce(er.pdf_path, '') AS record_pdf_path, coalesce(er.sha256, '') AS record_sha256,
	coalesce(er.ddc_path, '') AS record_ddc_path, coalesce(er.ddc_sha256, '') AS record_ddc_sha256,
	coalesce(u.iin, '') AS user_iin, a.iin, a.signer_name, a.cms, a.sigex_sign_id,
	coalesce(er.instructor_signed_at, ed.instructor_signed_at) AS instructor_signed_at,
	coalesce(er.instructor_iin, ed.instructor_iin, '') AS instructor_signer_iin,
	coalesce(er.instructor_name, ed.instructor_name, '') AS instructor_signer_name,
	coalesce(er.instructor_cms, ed.instructor_cms, '') AS instructor_cms,
	coalesce(er.instructor_sign_id, ed.instructor_sign_id) AS instructor_sign_id,
	coalesce(er.sigex_id, ed.sigex_id, '') AS sigex_id,
	a.material_body_snapshot AS material_body, a.material_file_snapshot AS material_file_path, a.material_sha256,
	a.internship_shifts, a.internship_from, a.internship_to, a.admitted_at, a.admitted_by_name
	FROM induction_assignments a JOIN induction_templates t ON t.id = a.template_id JOIN users u ON u.id = a.user_id
	LEFT JOIN departments d ON d.id = u.department_id LEFT JOIN users i ON i.id = a.instructor_id
	LEFT JOIN locations al ON al.id = a.location_id
	LEFT JOIN induction_records er ON er.assignment_id = a.id
	LEFT JOIN induction_docs ed ON ed.org_id = a.org_id AND ed.batch = a.batch`

const signingOperationSelect = `SELECT id, org_id, viewer_id, viewer_role, viewer_department_id, assignment_id, batch,
	expected_iin, instructor, qr_code, launch_link, data_url, sign_url, expires_at, stage,
	signer_iin, signer_name, cms, enriched_cms, evidence, ddc_path, ddc_sha256,
	sigex_id, sigex_sign_id, error, lease_until, created_at, updated_at
	FROM signing_operations`

func (s *Store) InductionTemplates(ctx context.Context, orgID int64) ([]domain.InductionTemplate, error) {
	return list[domain.InductionTemplate](ctx, s.db, `SELECT t.id, t.title, t.title_kk, t.kind, t.repeat_months, t.instructor_id,
		coalesce(i.name, '') AS instructor, (SELECT count(*) FROM induction_assignments a WHERE a.template_id = t.id) AS assigned
		FROM induction_templates t LEFT JOIN users i ON i.id = t.instructor_id WHERE t.org_id = $1 ORDER BY t.id DESC`, orgID)
}

func (s *Store) InductionTemplate(ctx context.Context, orgID, id int64) (domain.InductionTemplate, error) {
	return one[domain.InductionTemplate](ctx, s.db, `SELECT t.id, t.title, t.title_kk, t.kind, t.repeat_months, t.instructor_id,
		coalesce(i.name, '') AS instructor, t.body, t.file_path, t.require_geo, t.require_selfie
		FROM induction_templates t LEFT JOIN users i ON i.id = t.instructor_id WHERE t.id = $1 AND t.org_id = $2`, id, orgID)
}

func (s *Store) SaveInductionTemplate(ctx context.Context, orgID int64, t domain.InductionTemplate) error {
	args := []any{orgID, t.Title, t.TitleKK, t.Kind, t.RepeatMonths, t.InstructorID, t.Body, t.FilePath, t.RequireGeo, t.RequireSelfie}
	if t.ID == 0 {
		_, err := s.db.Exec(ctx, `INSERT INTO induction_templates (org_id, title, title_kk, kind, repeat_months, instructor_id,
			body, file_path, require_geo, require_selfie) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, args...)
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE induction_templates SET title=$2, title_kk=$3, kind=$4, repeat_months=$5, instructor_id=$6,
		body=$7, file_path=CASE WHEN $8 = '' THEN file_path ELSE $8 END, require_geo=$9, require_selfie=$10 WHERE org_id=$1 AND id=$11`, append(args, t.ID)...)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) UserAssignment(ctx context.Context, userID, id int64) (domain.Assignment, error) {
	return one[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.id = $1 AND a.user_id = $2`, id, userID)
}

func (s *Store) SignAssignment(ctx context.Context, userID, id int64, sig domain.Signature) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE induction_assignments SET signed_at = now(), signature = $3, selfie_path = $4, lat = $5, lng = $6, geo_ok = $7
		WHERE id = $1 AND user_id = $2 AND signed_at IS NULL`, id, userID, sig.Image, sig.Selfie, sig.Lat, sig.Lng, sig.GeoOK)
	return tag.RowsAffected() == 1, err
}

// AdmitAssignment записывает стажировку и допуск; область видимости та же, что у BatchAssignments.
func (s *Store) AdmitAssignment(ctx context.Context, viewer domain.User, id int64, in domain.AdmitInput, byName string) error {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE induction_assignments a SET internship_shifts = $3, internship_from = $4, internship_to = $5,
		admitted_at = now(), admitted_by = $6, admitted_by_name = $7
		FROM induction_templates t, users u
		WHERE a.id = $1 AND a.org_id = $2 AND t.id = a.template_id AND u.id = a.user_id
		AND t.kind = 'primary' AND a.knowledge_result = 'passed' AND a.signed_at IS NOT NULL AND a.admitted_at IS NULL
		AND EXISTS (SELECT 1 FROM induction_records ir WHERE ir.assignment_id=a.id AND ir.instructor_signed_at IS NOT NULL)
		AND ($8::bigint[] IS NULL OR u.department_id = ANY($8) OR ($9 = 'instructor' AND a.instructor_id = $6))`,
		id, viewer.OrgID, in.Shifts, in.From, in.To, viewer.ID, byName, deps, viewer.Role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) UsersForAssignment(ctx context.Context, viewer domain.User, userIDs []int64) ([]domain.User, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.User](ctx, s.db, `SELECT id, org_id, coalesce(phone, '') AS phone, coalesce(iin, '') AS iin,
		name, birth_year, position, department_id, location_id, role, active FROM users
		WHERE org_id = $1 AND id = ANY($2) AND active AND ($3::bigint[] IS NULL OR department_id = ANY($3))
		ORDER BY id`, viewer.OrgID, userIDs, deps)
}

func (s *Store) CreateAssignments(ctx context.Context, viewer domain.User, templateID int64, instructorID, locationID *int64,
	batch, reason, basis, workType, docs string, due time.Time, userIDs []int64, doc domain.InductionDoc) (int, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return 0, err
	}
	var n int
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Сотрудники фильтруются по организации и области видимости.
		tag, err := tx.Exec(ctx, `INSERT INTO induction_assignments
			(org_id, template_id, user_id, instructor_id, location_id, batch, reason, basis, work_type, docs, due_at,
			 worker_name_snapshot, worker_birth_year_snapshot, worker_position_snapshot, worker_department_snapshot,
			 instructor_name_snapshot, instructor_position_snapshot)
			SELECT $1, $2, u.id, $3, $4, $5, $6, $7, $8, $9, $10,
			 u.name, u.birth_year, u.position, coalesce(d.name, ''), coalesce(i.name, ''), coalesce(i.position, '')
			FROM users u LEFT JOIN users i ON i.id = $3 LEFT JOIN departments d ON d.id=u.department_id
			WHERE u.org_id = $1 AND u.id = ANY($11) AND ($12::bigint[] IS NULL OR u.department_id = ANY($12))`,
			viewer.OrgID, templateID, instructorID, locationID, batch, reason, basis, workType, docs, due, userIDs, deps)
		if err != nil {
			return err
		}
		n = int(tag.RowsAffected())
		_, err = tx.Exec(ctx, `INSERT INTO induction_docs (org_id, batch, pdf_path, sha256, allow_visitors)
			VALUES ($1,$2,$3,$4,$5)`, doc.OrgID, doc.Batch, doc.PDFPath, doc.SHA256, doc.AllowVisitors)
		return err
	})
	return n, err
}

func (s *Store) KioskState(ctx context.Context, viewer domain.User, batch string) (domain.KioskState, error) {
	if ok, err := s.BatchExists(ctx, viewer, batch); err != nil || !ok {
		if err != nil {
			return domain.KioskState{}, err
		}
		return domain.KioskState{}, domain.ErrNotFound
	}
	doc, err := one[domain.InductionDoc](ctx, s.db, `SELECT org_id, batch, pdf_path, sha256, sigex_id, allow_visitors,
		instructor_signed_at, instructor_iin, instructor_name, instructor_cms, instructor_sign_id, created_at
		FROM induction_docs WHERE org_id=$1 AND batch=$2`, viewer.OrgID, batch)
	if err != nil {
		return domain.KioskState{}, err
	}
	state := domain.KioskState{Doc: doc}
	err = s.db.QueryRow(ctx, `SELECT count(a.*)::int, count(a.signed_at)::int, coalesce(i.iin,''), coalesce(i.name,'')
		FROM induction_assignments a LEFT JOIN users i ON i.id=a.instructor_id
		WHERE a.org_id=$1 AND a.batch=$2 GROUP BY i.iin, i.name`, viewer.OrgID, batch).
		Scan(&state.Total, &state.Signed, &state.InstructorIIN, &state.InstructorName)
	state.InstructorPending = state.Signed == state.Total && doc.InstructorSignedAt == nil
	return state, err
}

func (s *Store) CanSignBatch(ctx context.Context, viewer domain.User, batch, iin string) error {
	state, err := s.KioskState(ctx, viewer, batch)
	if err != nil {
		return err
	}
	var assigned bool
	err = s.db.QueryRow(ctx, `SELECT exists(SELECT 1 FROM induction_assignments a JOIN users u ON u.id = a.user_id
		WHERE a.org_id=$1 AND a.batch=$2 AND u.iin=$3 AND a.signed_at IS NULL)`, viewer.OrgID, batch, iin).Scan(&assigned)
	if err != nil {
		return err
	}
	if assigned || (state.Signed < state.Total && state.Doc.AllowVisitors) ||
		(state.Signed == state.Total && state.Doc.InstructorSignedAt == nil && state.InstructorIIN == iin) {
		return nil
	}
	return domain.Invalid("ИИН подписанта не ожидается в этой партии")
}

func (s *Store) ApplyBatchEDS(ctx context.Context, viewer domain.User, batch string, sig domain.EDSSignature) error {
	if ok, err := s.BatchExists(ctx, viewer, batch); err != nil || !ok {
		if err != nil {
			return err
		}
		return domain.ErrNotFound
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var allow bool
		if err := tx.QueryRow(ctx, `SELECT allow_visitors FROM induction_docs WHERE org_id=$1 AND batch=$2 FOR UPDATE`, viewer.OrgID, batch).Scan(&allow); err != nil {
			return notFound(err)
		}
		tag, err := tx.Exec(ctx, `UPDATE induction_assignments a SET signed_at=now(), iin=$3, signer_name=$4, cms=$5, sigex_sign_id=$6
			FROM users u WHERE a.user_id=u.id AND a.org_id=$1 AND a.batch=$2 AND u.iin=$3 AND a.signed_at IS NULL`,
			viewer.OrgID, batch, sig.IIN, sig.Name, sig.CMS, sig.SignID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			_, err = tx.Exec(ctx, `UPDATE induction_docs SET sigex_id=$3 WHERE org_id=$1 AND batch=$2`, viewer.OrgID, batch, sig.SIGEXID)
			return err
		}
		var unsigned int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM induction_assignments WHERE org_id=$1 AND batch=$2 AND signed_at IS NULL`, viewer.OrgID, batch).Scan(&unsigned); err != nil {
			return err
		}
		if unsigned > 0 && allow {
			var userID int64
			err := tx.QueryRow(ctx, `INSERT INTO users (org_id, iin, name, role, active) VALUES ($1,$2,$3,'visitor',false)
				ON CONFLICT (org_id, iin) DO UPDATE SET name=excluded.name RETURNING id`, viewer.OrgID, sig.IIN, sig.Name).Scan(&userID)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO induction_assignments
				(org_id, template_id, user_id, instructor_id, location_id, batch, reason, basis, work_type, docs, assigned_at, due_at,
				signed_at, iin, signer_name, cms, sigex_sign_id)
				SELECT org_id, template_id, $3, instructor_id, location_id, batch, reason, basis, work_type, docs, assigned_at, due_at,
				now(), $4, $5, $6, $7 FROM induction_assignments WHERE org_id=$1 AND batch=$2 ORDER BY id LIMIT 1`,
				viewer.OrgID, batch, userID, sig.IIN, sig.Name, sig.CMS, sig.SignID)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE induction_docs SET sigex_id=$3 WHERE org_id=$1 AND batch=$2`, viewer.OrgID, batch, sig.SIGEXID)
			}
			return err
		}
		if unsigned > 0 {
			return domain.Invalid("ИИН подписанта не назначен на этот инструктаж")
		}
		var instructorIIN string
		err = tx.QueryRow(ctx, `SELECT coalesce(i.iin,'') FROM induction_assignments a LEFT JOIN users i ON i.id=a.instructor_id
			WHERE a.org_id=$1 AND a.batch=$2 LIMIT 1`, viewer.OrgID, batch).Scan(&instructorIIN)
		if err != nil {
			return err
		}
		if instructorIIN != sig.IIN {
			return domain.Invalid("Последним протокол подписывает инструктирующий")
		}
		tag, err = tx.Exec(ctx, `UPDATE induction_docs SET sigex_id=$3, instructor_signed_at=now(), instructor_iin=$4,
			instructor_name=$5, instructor_cms=$6, instructor_sign_id=$7
			WHERE org_id=$1 AND batch=$2 AND instructor_signed_at IS NULL`, viewer.OrgID, batch, sig.SIGEXID, sig.IIN, sig.Name, sig.CMS, sig.SignID)
		if err == nil && tag.RowsAffected() == 0 {
			return domain.Invalid("Инструктирующий уже подписал протокол")
		}
		return err
	})
}

func (s *Store) BatchExists(ctx context.Context, viewer domain.User, batch string) (bool, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.db.QueryRow(ctx, `SELECT exists(SELECT 1 FROM induction_assignments a JOIN users u ON u.id = a.user_id
		WHERE a.batch = $1 AND a.org_id = $2 AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5)))`,
		batch, viewer.OrgID, deps, viewer.Role, viewer.ID).Scan(&ok)
	return ok, err
}

func (s *Store) UnsignedInBatch(ctx context.Context, orgID int64, batch string) ([]domain.Notice, error) {
	return list[domain.Notice](ctx, s.db, `SELECT a.id, u.phone, t.title FROM induction_assignments a JOIN users u ON u.id = a.user_id
		JOIN induction_templates t ON t.id = a.template_id WHERE a.org_id = $1 AND a.batch = $2 AND a.signed_at IS NULL AND u.active`, orgID, batch)
}

func (s *Store) Batches(ctx context.Context, viewer domain.User) ([]domain.Batch, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Batch](ctx, s.db, `SELECT a.batch, t.title, t.kind, min(a.assigned_at) AS assigned_at, max(a.due_at) AS due_at,
		count(*) AS total, count(a.signed_at) AS signed
		FROM induction_assignments a JOIN induction_templates t ON t.id = a.template_id JOIN users u ON u.id = a.user_id
		WHERE a.org_id = $1 AND ($2::bigint[] IS NULL OR u.department_id = ANY($2) OR ($3 = 'instructor' AND a.instructor_id = $4))
		GROUP BY a.batch, t.title, t.kind ORDER BY min(a.assigned_at) DESC LIMIT 200`, viewer.OrgID, deps, viewer.Role, viewer.ID)
}

func (s *Store) BatchAssignments(ctx context.Context, viewer domain.User, batch string) ([]domain.Assignment, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.batch = $2
		AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5)) ORDER BY u.name`,
		viewer.OrgID, batch, deps, viewer.Role, viewer.ID)
}

func (s *Store) ScopedAssignment(ctx context.Context, viewer domain.User, id int64) (domain.Assignment, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return domain.Assignment{}, err
	}
	return one[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.id = $2
		AND ($3::bigint[] IS NULL OR u.department_id = ANY($3) OR ($4 = 'instructor' AND a.instructor_id = $5))`,
		viewer.OrgID, id, deps, viewer.Role, viewer.ID)
}

func (s *Store) SaveKnowledgeRecord(ctx context.Context, viewer domain.User, id int64, result, pdfPath, sha256 string) error {
	if !domain.ValidKnowledgeResult(result) {
		return domain.Invalid("Укажите результат проверки знаний")
	}
	if _, err := s.ScopedAssignment(ctx, viewer, id); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE induction_assignments SET knowledge_result=$3
			WHERE id=$1 AND org_id=$2 AND signed_at IS NULL AND knowledge_result=''`, id, viewer.OrgID, result)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return domain.Invalid("Результат уже зафиксирован или запись подписана")
		}
		_, err = tx.Exec(ctx, `INSERT INTO induction_records (assignment_id, org_id, pdf_path, sha256)
			VALUES ($1,$2,$3,$4)`, id, viewer.OrgID, pdfPath, sha256)
		return err
	})
}

func (s *Store) SnapshotBatchMaterial(ctx context.Context, viewer domain.User, batch, body, filePath, sha256 string) error {
	tag, err := s.db.Exec(ctx, `UPDATE induction_assignments SET material_body_snapshot=$3, material_file_snapshot=$4, material_sha256=$5
		WHERE org_id=$1 AND batch=$2 AND material_sha256=''`, viewer.OrgID, batch, body, filePath, sha256)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CreateSigningOperation(ctx context.Context, op domain.SigningOperationRecord) error {
	_, err := s.db.Exec(ctx, `INSERT INTO signing_operations
		(id, org_id, viewer_id, viewer_role, viewer_department_id, assignment_id, batch, expected_iin, instructor,
		 qr_code, launch_link, data_url, sign_url, expires_at, stage)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		op.ID, op.OrgID, op.ViewerID, op.ViewerRole, op.ViewerDepartmentID, op.AssignmentID, op.Batch, op.ExpectedIIN,
		op.Instructor, op.QRCode, op.LaunchLink, op.DataURL, op.SignURL, op.ExpiresAt, op.Stage)
	return err
}

func (s *Store) SigningOperation(ctx context.Context, orgID int64, id string) (domain.SigningOperationRecord, error) {
	return one[domain.SigningOperationRecord](ctx, s.db, signingOperationSelect+` WHERE org_id=$1 AND id=$2`, orgID, id)
}

func (s *Store) ClaimSigningOperation(ctx context.Context, orgID int64, id string) (domain.SigningOperationRecord, bool, error) {
	op, err := one[domain.SigningOperationRecord](ctx, s.db, `UPDATE signing_operations
		SET lease_until=now()+interval '3 minutes', updated_at=now()
		WHERE org_id=$1 AND id=$2 AND stage NOT IN ('done','failed') AND (lease_until IS NULL OR lease_until < now())
		RETURNING id, org_id, viewer_id, viewer_role, viewer_department_id, assignment_id, batch,
		expected_iin, instructor, qr_code, launch_link, data_url, sign_url, expires_at, stage,
		signer_iin, signer_name, cms, enriched_cms, evidence, ddc_path, ddc_sha256,
		sigex_id, sigex_sign_id, error, lease_until, created_at, updated_at`, orgID, id)
	if err == nil {
		return op, true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.SigningOperationRecord{}, false, err
	}
	op, err = s.SigningOperation(ctx, orgID, id)
	return op, false, err
}

func (s *Store) SaveSigningOperation(ctx context.Context, op domain.SigningOperationRecord) error {
	evidence := string(op.EvidenceJSON)
	if evidence == "" {
		evidence = "{}"
	}
	tag, err := s.db.Exec(ctx, `UPDATE signing_operations SET stage=$3, signer_iin=$4, signer_name=$5, cms=$6,
		enriched_cms=$7, evidence=$8::jsonb, ddc_path=$9, ddc_sha256=$10,
		sigex_id=$11, sigex_sign_id=$12, error=$13, lease_until=NULL, updated_at=now()
		WHERE org_id=$1 AND id=$2`, op.OrgID, op.ID, op.Stage, op.SignerIIN, op.SignerName, op.CMS,
		op.EnrichedCMS, evidence, op.DDCPath, op.DDCSHA256, op.SIGEXID, op.SIGEXSignID, op.Error)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) ReleaseSigningOperation(ctx context.Context, orgID int64, id string) error {
	tag, err := s.db.Exec(ctx, `UPDATE signing_operations SET lease_until=NULL, updated_at=now() WHERE org_id=$1 AND id=$2`, orgID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

func (s *Store) ApplyAssignmentEDS(ctx context.Context, viewer domain.User, id int64, instructor bool, sig domain.EDSSignature) error {
	if _, err := s.ScopedAssignment(ctx, viewer, id); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var workerIIN, instructorIIN, result string
		var signedAt, instructorSignedAt *time.Time
		var workerSignID, instructorSignID *int64
		err := tx.QueryRow(ctx, `SELECT coalesce(u.iin,''), coalesce(i.iin,''), a.knowledge_result, a.signed_at,
			r.instructor_signed_at, a.sigex_sign_id, r.instructor_sign_id
			FROM induction_assignments a JOIN users u ON u.id=a.user_id LEFT JOIN users i ON i.id=a.instructor_id
			JOIN induction_records r ON r.assignment_id=a.id WHERE a.id=$1 AND a.org_id=$2 FOR UPDATE OF a, r`, id, viewer.OrgID).
			Scan(&workerIIN, &instructorIIN, &result, &signedAt, &instructorSignedAt, &workerSignID, &instructorSignID)
		if err != nil {
			return notFound(err)
		}
		if result == "" {
			return domain.Invalid("Сначала зафиксируйте результат проверки знаний")
		}
		role := "worker"
		if instructor {
			role = "instructor"
		}
		tsp, ocsp := string(sig.Evidence.TSP), string(sig.Evidence.OCSP)
		policyIDs, keyUsages, extKeyUsages := sig.Evidence.PolicyIDs, sig.Evidence.KeyUsages, sig.Evidence.ExtKeyUsages
		if policyIDs == nil {
			policyIDs = []string{}
		}
		if keyUsages == nil {
			keyUsages = []string{}
		}
		if extKeyUsages == nil {
			extKeyUsages = []string{}
		}
		if tsp == "" {
			tsp = "{}"
		}
		if ocsp == "" {
			ocsp = "{}"
		}
		_, err = tx.Exec(ctx, `INSERT INTO induction_signatures
			(assignment_id, org_id, signer_role, sigex_id, sign_id, iin, bin, name, subject, issuer, serial_number, certificate,
			 certificate_from, certificate_until, stored_at, sign_algorithm, key_storage, policy_ids, key_usages,
			 ext_key_usages, cms_original, cms_enriched, tsp, ocsp)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb,$24::jsonb)
			ON CONFLICT (assignment_id, signer_role) DO NOTHING`, id, viewer.OrgID, role, sig.SIGEXID, sig.SignID,
			sig.IIN, sig.Evidence.BIN, sig.Name, sig.Evidence.Subject, sig.Evidence.Issuer, sig.Evidence.SerialNumber,
			sig.Evidence.Certificate, sig.Evidence.CertificateFrom, sig.Evidence.CertificateUntil, sig.Evidence.StoredAt, sig.Evidence.SignAlgorithm,
			sig.Evidence.KeyStorage, policyIDs, keyUsages, extKeyUsages,
			sig.CMS, sig.EnrichedCMS, tsp, ocsp)
		if err != nil {
			return err
		}
		if !instructor {
			if signedAt != nil {
				if workerSignID != nil && *workerSignID == sig.SignID {
					return nil
				}
				return domain.Invalid("Работник уже подписал запись")
			}
			if workerIIN != sig.IIN {
				return domain.Invalid("ИИН подписанта не совпадает с ИИН работника")
			}
			_, err = tx.Exec(ctx, `UPDATE induction_assignments SET signed_at=now(), iin=$3, signer_name=$4, cms=$5, sigex_sign_id=$6
				WHERE id=$1 AND org_id=$2`, id, viewer.OrgID, sig.IIN, sig.Name, sig.CMS, sig.SignID)
		} else {
			if signedAt == nil {
				return domain.Invalid("Сначала запись подписывает работник")
			}
			if instructorSignedAt != nil {
				if instructorSignID != nil && *instructorSignID == sig.SignID {
					return nil
				}
				return domain.Invalid("Инструктирующий уже подписал запись")
			}
			if instructorIIN != sig.IIN {
				return domain.Invalid("ИИН подписанта не совпадает с ИИН инструктирующего")
			}
			_, err = tx.Exec(ctx, `UPDATE induction_records SET instructor_signed_at=now(), instructor_iin=$3,
				instructor_name=$4, instructor_cms=$5, instructor_sign_id=$6, ddc_path=$7, ddc_sha256=$8
				WHERE assignment_id=$1 AND org_id=$2`, id, viewer.OrgID, sig.IIN, sig.Name, sig.CMS, sig.SignID,
				sig.DDCPath, sig.DDCSHA256)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE induction_records SET sigex_id=$3 WHERE assignment_id=$1 AND org_id=$2`, id, viewer.OrgID, sig.SIGEXID)
		return err
	})
}

func (s *Store) Journal(ctx context.Context, viewer domain.User, kinds []string, f domain.JournalFilter) ([]domain.Assignment, error) {
	deps, err := s.scopeDepts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.org_id = $1 AND a.signed_at IS NOT NULL
		AND (er.instructor_signed_at IS NOT NULL OR (er.assignment_id IS NULL AND ed.instructor_signed_at IS NOT NULL)) AND t.kind = ANY($2)
		AND a.signed_at >= coalesce(nullif($3, '')::date, '-infinity') AND a.signed_at < coalesce(nullif($4, '')::date + 1, 'infinity')
		AND ($5 = '' OR u.department_id::text = $5) AND ($6 = '' OR u.name ILIKE '%' || $6 || '%')
		AND ($7::bigint[] IS NULL OR u.department_id = ANY($7) OR ($8 = 'instructor' AND a.instructor_id = $9))
		ORDER BY a.signed_at`, viewer.OrgID, kinds, f.From, f.To, f.Department, f.Name, deps, viewer.Role, viewer.ID)
}

func (s *Store) UserAssignments(ctx context.Context, userID int64, signed bool) ([]domain.Assignment, error) {
	order := `a.due_at`
	if signed {
		order = `a.signed_at DESC`
	}
	return list[domain.Assignment](ctx, s.db, assignmentSelect+` WHERE a.user_id = $1 AND (a.signed_at IS NOT NULL) = $2 ORDER BY `+order, userID, signed)
}
