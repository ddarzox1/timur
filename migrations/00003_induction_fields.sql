-- +goose Up
ALTER TABLE induction_assignments
    ADD COLUMN location_id bigint REFERENCES locations ON DELETE SET NULL,
    ADD COLUMN basis text NOT NULL DEFAULT '',
    ADD COLUMN work_type text NOT NULL DEFAULT '',
    ADD COLUMN docs text NOT NULL DEFAULT '';

UPDATE induction_assignments a
SET location_id = u.location_id
FROM users u
WHERE u.id = a.user_id AND a.location_id IS NULL;

UPDATE induction_templates SET repeat_months = 6 WHERE kind = 'repeat' AND repeat_months = 0;

-- +goose Down
ALTER TABLE induction_assignments
    DROP COLUMN docs,
    DROP COLUMN work_type,
    DROP COLUMN basis,
    DROP COLUMN location_id;
