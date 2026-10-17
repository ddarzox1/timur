package domain

// Справочники. Ключи хранятся в БД, подписи — для интерфейса (переводятся в delivery).

type Category struct{ Key, Label, Icon string }

var Categories = []Category{
	{"height", "Работа на высоте", "🪜"},
	{"electric", "Электричество", "⚡"},
	{"machinery", "Техника и механизмы", "🚜"},
	{"fire", "Пожарная опасность", "🔥"},
	{"ppe", "СИЗ", "🦺"},
	{"housekeeping", "Порядок на площадке", "🧹"},
	{"other", "Другое", "⚠️"},
}

func CategoryByKey(k string) (Category, bool) {
	for _, c := range Categories {
		if c.Key == k {
			return c, true
		}
	}
	return Category{Key: k, Label: k, Icon: "⚠️"}, false
}

var (
	RiskLabels   = map[string]string{"low": "Низкий", "medium": "Средний", "high": "Высокий"}
	StatusLabels = map[string]string{"open": "Открыто", "in_progress": "В работе", "fixed": "Устранено", "rejected": "Отклонено"}
	KindOrder    = []string{"intro", "primary", "repeat", "unplanned", "target"}
	KindLabels   = map[string]string{"intro": "Вводный", "primary": "Первичный", "repeat": "Повторный", "unplanned": "Внеплановый", "target": "Целевой"}
	RoleLabels   = map[string]string{RoleWorker: "Сотрудник", RoleHSE: "Инженер ОТ", RoleAdmin: "Администратор"}
	FreqLabels   = map[string]string{"daily": "Ежедневно", "weekly": "Еженедельно", "once": "Разово"}
)

// RiskDays — срок устранения опасности по умолчанию, в днях.
var RiskDays = map[string]int{"high": 1, "medium": 3, "low": 7}

// WorkplaceKinds — виды, которые пишутся в журнал инструктажа на рабочем месте;
// вводный ведётся в отдельном журнале.
var WorkplaceKinds = []string{"primary", "repeat", "unplanned", "target"}
