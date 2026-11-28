-- +goose Up
ALTER TABLE signing_operations
    ADD COLUMN enriched_cms text NOT NULL DEFAULT '',
    ADD COLUMN evidence jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE induction_signatures (
    assignment_id    bigint NOT NULL REFERENCES induction_assignments ON DELETE RESTRICT,
    org_id           bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    signer_role      text NOT NULL CHECK (signer_role IN ('worker', 'instructor')),
    sigex_id         text NOT NULL,
    sign_id          bigint NOT NULL,
    iin              text NOT NULL,
    bin              text NOT NULL DEFAULT '',
    name             text NOT NULL DEFAULT '',
    subject          text NOT NULL DEFAULT '',
    issuer           text NOT NULL DEFAULT '',
    serial_number    text NOT NULL DEFAULT '',
    certificate_from bigint NOT NULL DEFAULT 0,
    certificate_until bigint NOT NULL DEFAULT 0,
    stored_at        bigint NOT NULL DEFAULT 0,
    sign_algorithm   text NOT NULL DEFAULT '',
    key_storage      text NOT NULL DEFAULT '',
    policy_ids       text[] NOT NULL DEFAULT '{}',
    key_usages       text[] NOT NULL DEFAULT '{}',
    ext_key_usages   text[] NOT NULL DEFAULT '{}',
    cms_original     text NOT NULL,
    cms_enriched     text NOT NULL,
    tsp              jsonb NOT NULL DEFAULT '{}'::jsonb,
    ocsp             jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (assignment_id, signer_role),
    UNIQUE (sigex_id, sign_id)
);
CREATE INDEX induction_signatures_org_created_idx ON induction_signatures (org_id, created_at DESC);

-- +goose Down
DROP TABLE induction_signatures;
ALTER TABLE signing_operations DROP COLUMN evidence, DROP COLUMN enriched_cms;
