package domain

// Справочники. Ключи хранятся в БД, подписи — для интерфейса (переводятся в delivery).

// Icon — id символа в web/static/icons.svg.
type Category struct{ Key, Label, Icon string }

var Categories = []Category{
	{"height", "Работа на высоте", "cat-height"},
	{"electric", "Электричество", "cat-electric"},
	{"machinery", "Техника и механизмы", "cat-machine"},
	{"fire", "Пожарная опасность", "cat-fire"},
	{"ppe", "СИЗ", "cat-ppe"},
	{"housekeeping", "Порядок на площадке", "cat-collapse"},
	{"other", "Другое", "cat-other"},
}

func CategoryByKey(k string) (Category, bool) {
	for _, c := range Categories {
		if c.Key == k {
			return c, true
		}
	}
	return Category{Key: k, Label: k, Icon: "cat-other"}, false
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
