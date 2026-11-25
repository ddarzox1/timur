-- +goose Up
ALTER TABLE induction_assignments
    ADD COLUMN worker_department_snapshot text NOT NULL DEFAULT '';

UPDATE induction_assignments a
SET worker_department_snapshot = coalesce(d.name, '')
FROM users u
LEFT JOIN departments d ON d.id = u.department_id
WHERE u.id = a.user_id;

-- +goose Down
ALTER TABLE induction_assignments DROP COLUMN worker_department_snapshot;
