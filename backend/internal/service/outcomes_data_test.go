package service

// Scenario — эталонный кейс кредитного решения: какой клиент, какие данные в анкете
// и какой исход обязана дать банковская логика. TestScenariosProduceExpectedOutcome
// прогоняет каждый кейс через предварительный расчёт и настоящий конвейер — регрессия правил сразу видна.
type Scenario struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"` // approved | counter_offer | need_info | rejected
	Tone    string `json:"tone"`    // positive | neutral | negative
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Phone   string `json:"phone"`
	Company string `json:"company"`
	Product string `json:"product_id"`
	Amount  int64  `json:"amount"`
	Term    int    `json:"term_months"`
	Revenue int64  `json:"annual_revenue"`      // выручка в анкете (как в ФНС, если не указано иное)
	Monthly int64  `json:"monthly_obligations"` // текущие платежи по кредитам в месяц
	// Level — на каком уровне анкеты банк выносит этот исход: 1 — предварительный расчёт, 2 — полный скоринг.
	Level      int      `json:"level"`
	Collateral int64    `json:"collateral"`    // залог-недвижимость на 2-м уровне, ₽ (0 — без залога)
	Guarantor  string   `json:"guarantor_inn"` // ИНН поручителя на 2-м уровне
	Why        []string `json:"why"`           // почему банк так решит — простыми словами
}

var Scenarios = []Scenario{
	{
		ID: "approve-best", Outcome: "approved", Tone: "positive",
		Title:   "Одобрение со сниженной ставкой",
		Summary: "Здоровый бизнес и небольшая нагрузка — банк одобряет запрошенные условия и снижает ставку на 1 п.п.",
		Phone:   "79000000001", Company: "ООО «Северный ветер»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 84_000_000, Level: 2,
		Why: []string{"Компания работает больше 3 лет", "Кредитная история без просрочек", "Платёж займёт ~2% месячной выручки — до 30% считается комфортным", "Высокий балл — ставка ниже базовой"},
	},
	{
		ID: "approve-equipment", Outcome: "approved", Tone: "positive",
		Title:   "Одобрение крупного кредита на оборудование",
		Summary: "15 млн ₽ на 5 лет под залог недвижимости: даже большая сумма проходит, если платёж укладывается в выручку.",
		Phone:   "79000000001", Company: "ООО «Северный ветер»",
		Product: "equipment", Amount: 15_000_000, Term: 60, Revenue: 84_000_000, Level: 2, Collateral: 25_000_000,
		Why: []string{"Платёж ~385 тыс. ₽ — около 6% месячной выручки", "Сумма меньше годовой выручки", "Хорошая кредитная история", "Залог покрывает кредит — ставка ниже"},
	},
	{
		ID: "approve-cheese", Outcome: "approved", Tone: "positive",
		Title:   "Тот же клиент, разумная сумма — одобрено",
		Summary: "У «Алтайских сыроварен» 3 млн ₽ одобряют сразу. Сравните со сценарием ниже — 20 млн ₽ тому же клиенту.",
		Phone:   "79000000002", Company: "ООО «Алтайские сыроварни»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 18_000_000, Level: 2,
		Why: []string{"Платёж ~152 тыс. ₽ — 10% месячной выручки", "Компания работает больше 3 лет", "Просрочек нет"},
	},
	{
		ID: "counter", Outcome: "counter_offer", Tone: "neutral",
		Title:   "Контрпредложение: банк подбирает сумму",
		Summary: "20 млн ₽ при выручке 18 млн ₽ в год и текущих платежах 200 тыс. ₽ — слишком большая нагрузка. Уже в предварительном расчёте банк предложит меньшую сумму.",
		Phone:   "79000000002", Company: "ООО «Алтайские сыроварни»",
		Product: "business-dev", Amount: 20_000_000, Term: 24, Revenue: 18_000_000, Monthly: 200_000, Level: 1,
		Why: []string{"Платёж вместе с текущими займёт больше 50% выручки", "Сумма больше годовой выручки", "Банк уменьшает сумму, чтобы платёж уложился в 50%"},
	},
	{
		ID: "need-info", Outcome: "need_info", Tone: "neutral",
		Title:   "Уточнение: выручка не сходится с ФНС",
		Summary: "В анкете 50 млн ₽ выручки, а по данным налоговой — 18 млн ₽. Предварительный расчёт попросит проверить цифру и пересчитает после исправления.",
		Phone:   "79000000002", Company: "ООО «Алтайские сыроварни»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 50_000_000, Level: 1,
		Why: []string{"Указанная выручка больше данных ФНС в 1,5 раза и более", "Автоматически оценить такую заявку нельзя"},
	},
	{
		ID: "reject-overdue", Outcome: "rejected", Tone: "negative",
		Title:   "Отказ: просрочки в кредитной истории",
		Summary: "Предварительный расчёт без кредитной истории проходит, но на полном скоринге видны просрочки до 45 дней и 4 действующих кредита — балл ниже порога.",
		Phone:   "79000000003", Company: "ООО «Сибирский кран»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 30_000_000, Monthly: 300_000, Level: 2,
		Why: []string{"Просрочки до 45 дней — главный фактор риска", "Рейтинг БКИ ниже среднего", "4 действующих кредита"},
	},
	{
		ID: "reject-liquidated", Outcome: "rejected", Tone: "negative",
		Title:   "Отказ: компания ликвидирована",
		Summary: "По данным ЕГРЮЛ компания не действует — отказ уже в предварительном расчёте, до входа через Госуслуги.",
		Phone:   "79000000004", Company: "ООО «Закрытый горизонт»",
		Product: "business-dev", Amount: 1_000_000, Term: 24, Revenue: 10_000_000, Level: 1,
		Why: []string{"Кредит можно выдать только действующей компании"},
	},
	{
		ID: "need-info-guarantor", Outcome: "need_info", Tone: "neutral",
		Title:   "Уточнение на 2-м уровне: поручитель с просрочками",
		Summary: "8 млн ₽ без залога под поручительство человека с просрочками в БКИ — банк попросит другое обеспечение.",
		Phone:   "79000000001", Company: "ООО «Северный ветер»",
		Product: "business-dev", Amount: 8_000_000, Term: 24, Revenue: 84_000_000, Level: 2, Guarantor: "540100000009",
		Why: []string{"Для кредита больше 5 млн ₽ нужен залог хотя бы на половину суммы или поручитель", "У поручителя есть просрочки — такое поручительство не принимается"},
	},
	{
		ID: "approve-collateral", Outcome: "approved", Tone: "positive",
		Title:   "Залог на всю сумму — ставка ниже",
		Summary: "8 млн ₽ под залог недвижимости на 15 млн ₽: в зачёт 10,5 млн ₽ — больше суммы кредита, ставка ниже ещё на 1 п.п.",
		Phone:   "79000000001", Company: "ООО «Северный ветер»",
		Product: "business-dev", Amount: 8_000_000, Term: 24, Revenue: 84_000_000, Level: 2, Collateral: 15_000_000,
		Why: []string{"Недвижимость принимается в зачёт на 70% оценки", "Залог покрывает весь кредит — ставка на 1 п.п. ниже"},
	},
}
