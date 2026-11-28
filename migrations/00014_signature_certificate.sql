-- +goose Up
ALTER TABLE induction_signatures ADD COLUMN certificate text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE induction_signatures DROP COLUMN certificate;
