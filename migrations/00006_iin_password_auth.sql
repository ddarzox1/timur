-- +goose Up
ALTER TABLE users ADD COLUMN password_hash text NOT NULL DEFAULT '';

-- Вход выполняется только по ИИН, поэтому среди учётных записей с паролем
-- один ИИН не может обозначать двух активных пользователей. Старые строки без
-- пароля и неактивные посетители не блокируют безопасное обновление схемы.
CREATE UNIQUE INDEX users_login_iin_key ON users (iin)
    WHERE iin IS NOT NULL AND active AND password_hash <> '';

-- Одноразовые коды больше не участвуют во входе. Таблицу оставляем до следующей
-- очистительной миграции, чтобы откат версии приложения не терял данные.

-- +goose Down
DROP INDEX users_login_iin_key;
ALTER TABLE users DROP COLUMN password_hash;
