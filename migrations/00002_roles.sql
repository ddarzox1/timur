-- +goose Up
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ALTER COLUMN phone DROP NOT NULL;
ALTER TABLE users ADD COLUMN iin text;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN (
    'worker', 'instructor', 'section_head', 'shop_head', 'hse', 'admin', 'visitor'
));
ALTER TABLE users ADD CONSTRAINT users_org_iin_key UNIQUE (org_id, iin);

-- +goose Down
DELETE FROM users WHERE role = 'visitor';
UPDATE users SET role = 'worker' WHERE role IN ('instructor', 'section_head', 'shop_head');
ALTER TABLE users DROP CONSTRAINT users_org_iin_key;
ALTER TABLE users DROP COLUMN iin;
ALTER TABLE users ALTER COLUMN phone SET NOT NULL;
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('worker', 'hse', 'admin'));
