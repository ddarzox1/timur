-- +goose Up
ALTER TABLE signing_operations
    ADD COLUMN ddc_path text NOT NULL DEFAULT '',
    ADD COLUMN ddc_sha256 text NOT NULL DEFAULT '';
ALTER TABLE induction_records
    ADD COLUMN ddc_path text NOT NULL DEFAULT '',
    ADD COLUMN ddc_sha256 text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE induction_records DROP COLUMN ddc_sha256, DROP COLUMN ddc_path;
ALTER TABLE signing_operations DROP COLUMN ddc_sha256, DROP COLUMN ddc_path;
