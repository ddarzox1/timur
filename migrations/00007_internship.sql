-- +goose Up
-- Стажировка и допуск к самостоятельной работе после первичного инструктажа (журнал на рабочем месте).
ALTER TABLE induction_assignments
    ADD COLUMN internship_shifts int CHECK (internship_shifts BETWEEN 0 AND 14),
    ADD COLUMN internship_from   date,
    ADD COLUMN internship_to     date,
    ADD COLUMN admitted_at       timestamptz,
    ADD COLUMN admitted_by       bigint REFERENCES users ON DELETE SET NULL,
    -- ФИО и должность на момент допуска: запись журнала не меняется при правке сотрудника.
    ADD COLUMN admitted_by_name  text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE induction_assignments
    DROP COLUMN admitted_by_name,
    DROP COLUMN admitted_by,
    DROP COLUMN admitted_at,
    DROP COLUMN internship_to,
    DROP COLUMN internship_from,
    DROP COLUMN internship_shifts;
