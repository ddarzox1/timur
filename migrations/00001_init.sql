-- +goose Up
CREATE TABLE orgs (
    id               bigserial PRIMARY KEY,
    name             text NOT NULL,
    bin              text NOT NULL DEFAULT '',
    last_injury_at   date,
    repeat_warn_days int  NOT NULL DEFAULT 14,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE departments (
    id        bigserial PRIMARY KEY,
    org_id    bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    parent_id bigint REFERENCES departments ON DELETE SET NULL,
    name      text NOT NULL
);

CREATE TABLE locations (
    id              bigserial PRIMARY KEY,
    org_id          bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    name            text NOT NULL,
    address         text NOT NULL DEFAULT '',
    lat             double precision,
    lng             double precision,
    radius_m        int  NOT NULL DEFAULT 300,
    responsible_id  bigint,
    evacuation_path text NOT NULL DEFAULT ''
);

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    org_id        bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    phone         text NOT NULL UNIQUE, -- ponytail: один номер = одна компания; выбор компании, когда появятся подрядчики
    name          text NOT NULL,
    birth_year    int,
    position      text NOT NULL DEFAULT '',
    department_id bigint REFERENCES departments ON DELETE SET NULL,
    location_id   bigint REFERENCES locations ON DELETE SET NULL,
    role          text NOT NULL DEFAULT 'worker' CHECK (role IN ('worker', 'hse', 'admin')),
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE locations ADD FOREIGN KEY (responsible_id) REFERENCES users ON DELETE SET NULL;

CREATE TABLE otp_codes (
    id         bigserial PRIMARY KEY,
    phone      text NOT NULL,
    code_hash  text NOT NULL,
    attempts   int NOT NULL DEFAULT 0,
    used       boolean NOT NULL DEFAULT false,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON otp_codes (phone, created_at DESC);

CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE induction_templates (
    id             bigserial PRIMARY KEY,
    org_id         bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    title          text NOT NULL,
    title_kk       text NOT NULL DEFAULT '',
    kind           text NOT NULL CHECK (kind IN ('intro', 'primary', 'repeat', 'unplanned', 'target')),
    repeat_months  int  NOT NULL DEFAULT 0,
    instructor_id  bigint REFERENCES users ON DELETE SET NULL,
    body           text NOT NULL DEFAULT '',
    file_path      text NOT NULL DEFAULT '',
    require_geo    boolean NOT NULL DEFAULT false,
    require_selfie boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Строки журнала инструктажей: юридическая запись, не удаляется вместе с шаблоном.
CREATE TABLE induction_assignments (
    id            bigserial PRIMARY KEY,
    org_id        bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    template_id   bigint NOT NULL REFERENCES induction_templates ON DELETE RESTRICT,
    user_id       bigint NOT NULL REFERENCES users ON DELETE RESTRICT,
    instructor_id bigint REFERENCES users ON DELETE SET NULL,
    batch         text NOT NULL,
    reason        text NOT NULL DEFAULT '',
    assigned_at   timestamptz NOT NULL DEFAULT now(),
    due_at        date NOT NULL,
    signed_at     timestamptz,
    signature     text NOT NULL DEFAULT '',
    selfie_path   text NOT NULL DEFAULT '',
    lat           double precision,
    lng           double precision,
    geo_ok        boolean
);
CREATE INDEX ON induction_assignments (org_id, user_id);

CREATE TABLE hazards (
    id             bigserial PRIMARY KEY,
    org_id         bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    author_id      bigint REFERENCES users ON DELETE SET NULL,
    location_id    bigint REFERENCES locations ON DELETE SET NULL,
    place          text NOT NULL DEFAULT '',
    category       text NOT NULL,
    risk           text NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
    description    text NOT NULL,
    responsible_id bigint REFERENCES users ON DELETE SET NULL,
    due_at         date,
    status         text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'fixed', 'rejected')),
    action         text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    fixed_at       timestamptz
);
CREATE INDEX ON hazards (org_id, status);

CREATE TABLE hazard_photos (
    id        bigserial PRIMARY KEY,
    hazard_id bigint NOT NULL REFERENCES hazards ON DELETE CASCADE,
    kind      text NOT NULL CHECK (kind IN ('before', 'after')),
    path      text NOT NULL
);

CREATE TABLE hazard_events (
    id         bigserial PRIMARY KEY,
    hazard_id  bigint NOT NULL REFERENCES hazards ON DELETE CASCADE,
    user_id    bigint REFERENCES users ON DELETE SET NULL,
    text       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE checklist_templates (
    id          bigserial PRIMARY KEY,
    org_id      bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    title       text NOT NULL,
    location_id bigint REFERENCES locations ON DELETE SET NULL,
    frequency   text NOT NULL DEFAULT 'daily' CHECK (frequency IN ('daily', 'weekly', 'once')),
    items       jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE checklist_runs (
    id          bigserial PRIMARY KEY,
    org_id      bigint NOT NULL REFERENCES orgs ON DELETE CASCADE,
    template_id bigint NOT NULL REFERENCES checklist_templates ON DELETE CASCADE,
    user_id     bigint REFERENCES users ON DELETE SET NULL,
    location_id bigint REFERENCES locations ON DELETE SET NULL,
    answers     jsonb NOT NULL,
    issues      int NOT NULL DEFAULT 0,
    total       int NOT NULL DEFAULT 0,
    signature   text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE pilot_requests (
    id         bigserial PRIMARY KEY,
    company    text NOT NULL,
    name       text NOT NULL,
    phone      text NOT NULL,
    employees  text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE pilot_requests, checklist_runs, checklist_templates, hazard_events, hazard_photos, hazards,
    induction_assignments, induction_templates, sessions, otp_codes, users, locations, departments, orgs;
