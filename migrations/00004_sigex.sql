-- +goose Up
CREATE TABLE induction_docs (
    org_id               bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    batch                text NOT NULL,
    pdf_path             text NOT NULL,
    sha256               text NOT NULL,
    sigex_id             text NOT NULL DEFAULT '',
    allow_visitors       boolean NOT NULL DEFAULT false,
    instructor_signed_at timestamptz,
    instructor_iin       text NOT NULL DEFAULT '',
    instructor_name      text NOT NULL DEFAULT '',
    instructor_cms       text NOT NULL DEFAULT '',
    instructor_sign_id   bigint,
    created_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, batch)
);
CREATE INDEX induction_docs_org_created_idx ON induction_docs (org_id, created_at DESC);

ALTER TABLE induction_assignments
    ADD COLUMN iin text NOT NULL DEFAULT '',
    ADD COLUMN signer_name text NOT NULL DEFAULT '',
    ADD COLUMN cms text NOT NULL DEFAULT '',
    ADD COLUMN sigex_sign_id bigint;

-- +goose Down
ALTER TABLE induction_assignments
    DROP COLUMN sigex_sign_id,
    DROP COLUMN cms,
    DROP COLUMN signer_name,
    DROP COLUMN iin;
DROP TABLE induction_docs;
