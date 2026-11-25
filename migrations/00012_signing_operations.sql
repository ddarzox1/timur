-- +goose Up
CREATE TABLE signing_operations (
    id                   text PRIMARY KEY,
    org_id               bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    viewer_id            bigint NOT NULL,
    viewer_role          text NOT NULL,
    viewer_department_id bigint,
    assignment_id        bigint NOT NULL REFERENCES induction_assignments ON DELETE RESTRICT,
    batch                text NOT NULL,
    expected_iin         text NOT NULL,
    instructor           boolean NOT NULL DEFAULT false,
    qr_code              text NOT NULL DEFAULT '',
    launch_link          text NOT NULL DEFAULT '',
    data_url             text NOT NULL DEFAULT '',
    sign_url             text NOT NULL DEFAULT '',
    expires_at           timestamptz NOT NULL,
    stage                text NOT NULL DEFAULT 'pending'
        CHECK (stage IN ('pending', 'signed', 'registered', 'done', 'failed')),
    signer_iin           text NOT NULL DEFAULT '',
    signer_name          text NOT NULL DEFAULT '',
    cms                  text NOT NULL DEFAULT '',
    sigex_id             text NOT NULL DEFAULT '',
    sigex_sign_id        bigint,
    error                text NOT NULL DEFAULT '',
    lease_until          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX signing_operations_org_updated_idx ON signing_operations (org_id, updated_at DESC);
CREATE INDEX signing_operations_active_idx ON signing_operations (stage, lease_until)
    WHERE stage NOT IN ('done', 'failed');

-- +goose Down
DROP TABLE signing_operations;
