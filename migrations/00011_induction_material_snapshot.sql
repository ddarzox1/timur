-- +goose Up
ALTER TABLE induction_assignments
    ADD COLUMN material_body_snapshot text NOT NULL DEFAULT '',
    ADD COLUMN material_file_snapshot text NOT NULL DEFAULT '',
    ADD COLUMN material_sha256 text NOT NULL DEFAULT '';

UPDATE induction_assignments a
SET material_body_snapshot = t.body,
    material_file_snapshot = t.file_path
FROM induction_templates t
WHERE t.id = a.template_id;

-- +goose Down
ALTER TABLE induction_assignments
    DROP COLUMN material_sha256,
    DROP COLUMN material_file_snapshot,
    DROP COLUMN material_body_snapshot;
