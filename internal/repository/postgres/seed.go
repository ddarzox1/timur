package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Seed создаёт демо-организацию ТОО «Строй-Инвест», если база пустая.
func Seed(ctx context.Context, db *pgxpool.Pool) error {
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM orgs`).Scan(&n); err != nil || n > 0 {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var org, depBuild, depStore, depAdm, locAlatau, locStore int64
		var qerr error
		q := func(dst *int64, sql string, args ...any) {
			if qerr == nil {
				qerr = tx.QueryRow(ctx, sql+` RETURNING id`, args...).Scan(dst)
			}
		}
		q(&org, `INSERT INTO orgs (name, bin, created_at) VALUES ('ТОО «Строй-Инвест»', '180340012345', now() - interval '128 days')`)
		q(&depAdm, `INSERT INTO departments (org_id, name) VALUES ($1, 'Администрация')`, org)
		q(&depBuild, `INSERT INTO departments (org_id, parent_id, name) VALUES ($1, $2, 'Строительный участок №1')`, org, depAdm)
		q(&depStore, `INSERT INTO departments (org_id, parent_id, name) VALUES ($1, $2, 'Склад')`, org, depAdm)
		q(&locAlatau, `INSERT INTO locations (org_id, name, address, lat, lng) VALUES ($1, 'Объект ЖК Алатау, Алматы', 'пр. Аль-Фараби, 77', 43.2389, 76.8897)`, org)
		q(&locStore, `INSERT INTO locations (org_id, name, address, lat, lng) VALUES ($1, 'Склад №2, Караганда', 'ул. Складская, 12', 49.8047, 73.1094)`, org)
		var aigerim, erlan, daniyar, assel int64
		u := `INSERT INTO users (org_id, phone, name, birth_year, position, department_id, location_id, role) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
		q(&aigerim, u, org, "77010000001", "Айгерим Нурланова", 1990, "Инженер по охране труда", depAdm, locAlatau, "admin")
		q(&erlan, u, org, "77010000002", "Ерлан Сапаров", 1988, "Монтажник", depBuild, locAlatau, "worker")
		q(&daniyar, u, org, "77010000003", "Данияр Ахметов", 1985, "Прораб", depBuild, locAlatau, "worker")
		q(&assel, u, org, "77010000004", "Асель Жумабаева", 1996, "Кладовщик", depStore, locStore, "worker")
		if qerr != nil {
			return qerr
		}
		if _, err := tx.Exec(ctx, `UPDATE locations SET responsible_id = $1 WHERE id = $2`, daniyar, locAlatau); err != nil {
			return err
		}
		var tIntro, tHeight int64
		q(&tIntro, `INSERT INTO induction_templates (org_id, title, title_kk, kind, instructor_id, body) VALUES ($1, 'Вводный инструктаж по безопасности и охране труда', 'Еңбек қауіпсіздігі және еңбекті қорғау бойынша кіріспе нұсқама', 'intro', $2, $3)`,
			org, aigerim, "1. Общие сведения о предприятии и характерных опасностях.\n2. Права и обязанности работника по охране труда.\n3. Средства индивидуальной защиты: выдача, порядок применения.\n4. Порядок действий при несчастном случае и пожаре.\n5. Первая помощь пострадавшему.")
		q(&tHeight, `INSERT INTO induction_templates (org_id, title, title_kk, kind, repeat_months, instructor_id, body, require_geo) VALUES ($1, 'Повторный инструктаж: работа на высоте', 'Қайталама нұсқама: биіктіктегі жұмыс', 'repeat', 6, $2, $3, true)`,
			org, aigerim, "1. Работы на высоте — работы на высоте 1,8 м и более от уровня пола.\n2. Обязательно применение страховочной привязи, закреплённой к анкерной точке.\n3. Перед началом работ осмотреть леса и ограждения, о неисправностях сообщить прорабу.\n4. Запрещено работать на высоте при ветре свыше 15 м/с, гололёде, грозе.\n5. Инструменты переносить в сумке, не бросать вниз.")
		a := `INSERT INTO induction_assignments (org_id, template_id, user_id, instructor_id, batch, due_at) VALUES ($1,$2,$3,$4,$5, current_date + $6::int)`
		for _, row := range [][]any{{tHeight, erlan, "seed-1", 3}, {tHeight, daniyar, "seed-1", 3}, {tIntro, assel, "seed-2", -1}} {
			if _, err := tx.Exec(ctx, a, org, row[0], row[1], aigerim, row[2], row[3]); err != nil {
				return err
			}
		}
		items := `[{"type":"section","text":"Леса и ограждения"},
			{"type":"question","text":"Настил лесов без зазоров и повреждений","hazard_on_no":true,"category":"height"},
			{"type":"question","text":"Перила и бортовые доски установлены","hazard_on_no":true,"category":"height"},
			{"type":"question","text":"Лестницы закреплены","hazard_on_no":true,"category":"height"},
			{"type":"section","text":"Площадка"},
			{"type":"question","text":"Проходы свободны от мусора и материалов","hazard_on_no":true,"category":"housekeeping"},
			{"type":"question","text":"Электрокабели подвешены, без повреждений","hazard_on_no":true,"category":"electric"},
			{"type":"question","text":"Все работники в каске и жилете","hazard_on_no":true,"category":"ppe"}]`
		if _, err := tx.Exec(ctx, `INSERT INTO checklist_templates (org_id, title, location_id, items) VALUES ($1, 'Ежедневный осмотр лесов', $2, $3)`, org, locAlatau, items); err != nil {
			return err
		}
		var h1, h2 int64
		q(&h1, `INSERT INTO hazards (org_id, author_id, location_id, place, category, risk, description, responsible_id, due_at, created_at) VALUES ($1,$2,$3,'Секция Б, 5 этаж','height','high','Нет ограждения на краю перекрытия',$4, current_date - 1, now() - interval '2 days')`, org, erlan, locAlatau, daniyar)
		q(&h2, `INSERT INTO hazards (org_id, author_id, location_id, place, category, risk, description, responsible_id, due_at, status) VALUES ($1,$2,$3,'Ворота №3','housekeeping','medium','Поддоны загромождают проход к эвакуационному выходу',$4, current_date + 2, 'in_progress')`, org, assel, locStore, aigerim)
		if qerr != nil {
			return qerr
		}
		for _, h := range []int64{h1, h2} {
			if _, err := tx.Exec(ctx, `INSERT INTO hazard_events (hazard_id, user_id, text) SELECT id, author_id, 'Создано' FROM hazards WHERE id = $1`, h); err != nil {
				return err
			}
		}
		return nil
	})
}
