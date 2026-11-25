-- +goose Up
-- Юридически значимая запись формируется отдельно на каждого инструктируемого.
-- Снимки не дают журналу измениться после редактирования карточек сотрудников.
ALTER TABLE induction_assignments
    ADD COLUMN knowledge_result text NOT NULL DEFAULT ''
        CHECK (knowledge_result IN ('', 'passed', 'failed')),
    ADD COLUMN worker_name_snapshot text NOT NULL DEFAULT '',
    ADD COLUMN worker_birth_year_snapshot int,
    ADD COLUMN worker_position_snapshot text NOT NULL DEFAULT '',
    ADD COLUMN instructor_name_snapshot text NOT NULL DEFAULT '',
    ADD COLUMN instructor_position_snapshot text NOT NULL DEFAULT '';

UPDATE induction_assignments a
SET worker_name_snapshot = u.name,
    worker_birth_year_snapshot = u.birth_year,
    worker_position_snapshot = u.position,
    instructor_name_snapshot = coalesce((SELECT i.name FROM users i WHERE i.id = a.instructor_id), ''),
    instructor_position_snapshot = coalesce((SELECT i.position FROM users i WHERE i.id = a.instructor_id), '')
FROM users u
WHERE u.id = a.user_id;

CREATE TABLE induction_records (
    assignment_id        bigint PRIMARY KEY REFERENCES induction_assignments ON DELETE RESTRICT,
    org_id               bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    pdf_path             text NOT NULL,
    sha256               text NOT NULL,
    sigex_id             text NOT NULL DEFAULT '',
    instructor_signed_at timestamptz,
    instructor_iin       text NOT NULL DEFAULT '',
    instructor_name      text NOT NULL DEFAULT '',
    instructor_cms       text NOT NULL DEFAULT '',
    instructor_sign_id   bigint,
    created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX induction_records_org_created_idx ON induction_records (org_id, created_at DESC);

-- +goose Down
DROP TABLE induction_records;
ALTER TABLE induction_assignments
    DROP COLUMN instructor_position_snapshot,
    DROP COLUMN instructor_name_snapshot,
    DROP COLUMN worker_position_snapshot,
    DROP COLUMN worker_birth_year_snapshot,
    DROP COLUMN worker_name_snapshot,
    DROP COLUMN knowledge_result;
