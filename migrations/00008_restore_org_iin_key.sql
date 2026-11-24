-- +goose Up
-- Подпись посетителя в киоске делает ON CONFLICT (org_id, iin). В части баз ключ из 00002
-- потерялся, и туда успели попасть дубли. Неактивные дубли теряют ИИН (журнал хранит ИИН
-- подписанта в induction_assignments.iin), затем ключ восстанавливается. Активные дубли
-- не трогаем: ключ не создаётся, сервер пишет предупреждение и продолжает работу.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('users_org_iin_key') IS NOT NULL THEN
        RETURN;
    END IF;
    UPDATE users u SET iin = NULL
    WHERE NOT u.active AND u.iin IS NOT NULL AND EXISTS (
        SELECT 1 FROM users o WHERE o.org_id = u.org_id AND o.iin = u.iin AND o.id <> u.id AND (o.active OR o.id > u.id));
    IF EXISTS (SELECT 1 FROM users WHERE iin IS NOT NULL GROUP BY org_id, iin HAVING count(*) > 1) THEN
        RAISE WARNING 'users_org_iin_key не создан: в организации есть активные дубли ИИН, исправьте вручную';
    ELSE
        CREATE UNIQUE INDEX users_org_iin_key ON users (org_id, iin);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
