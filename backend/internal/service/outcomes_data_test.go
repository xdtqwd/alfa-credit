package service

// Scenario — эталонный кейс кредитного решения: какой клиент, какие данные в анкете
// и какой исход обязана дать банковская логика. TestScenariosProduceExpectedOutcome
// прогоняет каждый кейс через настоящий конвейер и скоринг — регрессия правил сразу видна.
type Scenario struct {
	ID      string   `json:"id"`
	Outcome string   `json:"outcome"` // approved | counter_offer | need_info | rejected
	Tone    string   `json:"tone"`    // positive | neutral | negative
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Phone   string   `json:"phone"`
	Company string   `json:"company"`
	Product string   `json:"product_id"`
	Amount  int64    `json:"amount"`
	Term    int      `json:"term_months"`
	Revenue int64    `json:"annual_revenue"`      // выручка в анкете (как в ФНС, если не указано иное)
	Monthly int64    `json:"monthly_obligations"` // текущие платежи по кредитам в месяц
	Why     []string `json:"why"`                 // почему банк так решит — простыми словами
}

var Scenarios = []Scenario{
	{
		ID: "approve-best", Outcome: "approved", Tone: "positive",
		Title:   "Одобрение со сниженной ставкой",
		Summary: "Здоровый бизнес и небольшая нагрузка — банк одобряет запрошенные условия и снижает ставку на 1 п.п.",
		Phone:   "79000000001", Company: "ООО «Северный ветер»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 84_000_000,
		Why: []string{"Компания работает больше 3 лет", "Кредитная история без просрочек", "Платёж займёт ~2% месячной выручки — до 30% считается комфортным", "Высокий балл — ставка ниже базовой"},
	},
	{
		ID: "approve-equipment", Outcome: "approved", Tone: "positive",
		Title:   "Одобрение крупного кредита на оборудование",
		Summary: "15 млн ₽ на 5 лет: даже большая сумма проходит, если платёж укладывается в выручку.",
		Phone:   "79000000001", Company: "ООО «Северный ветер»",
		Product: "equipment", Amount: 15_000_000, Term: 60, Revenue: 84_000_000,
		Why: []string{"Платёж ~385 тыс. ₽ — около 6% месячной выручки", "Сумма меньше годовой выручки", "Хорошая кредитная история"},
	},
	{
		ID: "approve-cheese", Outcome: "approved", Tone: "positive",
		Title:   "Тот же клиент, разумная сумма — одобрено",
		Summary: "У «Алтайских сыроварен» 3 млн ₽ одобряют сразу. Сравните со сценарием ниже — 20 млн ₽ тому же клиенту.",
		Phone:   "79000000002", Company: "ООО «Алтайские сыроварни»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 18_000_000,
		Why: []string{"Платёж ~152 тыс. ₽ — 10% месячной выручки", "Компания работает больше 3 лет", "Просрочек нет"},
	},
	{
		ID: "counter", Outcome: "counter_offer", Tone: "neutral",
		Title:   "Контрпредложение: банк подбирает сумму",
		Summary: "20 млн ₽ при выручке 18 млн ₽ в год и текущих платежах 200 тыс. ₽ — слишком большая нагрузка. Банк предложит меньшую сумму.",
		Phone:   "79000000002", Company: "ООО «Алтайские сыроварни»",
		Product: "business-dev", Amount: 20_000_000, Term: 24, Revenue: 18_000_000, Monthly: 200_000,
		Why: []string{"Платёж вместе с текущими займёт больше 50% выручки", "Сумма больше годовой выручки", "Банк уменьшает сумму, чтобы платёж уложился в 50%"},
	},
	{
		ID: "need-info", Outcome: "need_info", Tone: "neutral",
		Title:   "Уточнение: выручка не сходится с ФНС",
		Summary: "В анкете 50 млн ₽ выручки, а по данным налоговой — 18 млн ₽. Банк попросит проверить цифру; после исправления заявка снова уходит в конвейер.",
		Phone:   "79000000002", Company: "ООО «Алтайские сыроварни»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 50_000_000,
		Why: []string{"Указанная выручка больше данных ФНС в 1,5 раза и более", "Автоматически оценить такую заявку нельзя"},
	},
	{
		ID: "reject-overdue", Outcome: "rejected", Tone: "negative",
		Title:   "Отказ: просрочки в кредитной истории",
		Summary: "У «Сибирского крана» просрочки до 45 дней и 4 действующих кредита — балл ниже порога.",
		Phone:   "79000000003", Company: "ООО «Сибирский кран»",
		Product: "business-dev", Amount: 3_000_000, Term: 24, Revenue: 30_000_000, Monthly: 300_000,
		Why: []string{"Просрочки до 45 дней — главный фактор риска", "Рейтинг БКИ ниже среднего", "4 действующих кредита"},
	},
	{
		ID: "reject-liquidated", Outcome: "rejected", Tone: "negative",
		Title:   "Отказ: компания ликвидирована",
		Summary: "По данным ЕГРЮЛ компания не действует — отказ ещё на проверке данных, до скоринга.",
		Phone:   "79000000004", Company: "ООО «Закрытый горизонт»",
		Product: "business-dev", Amount: 1_000_000, Term: 24, Revenue: 10_000_000,
		Why: []string{"Кредит можно выдать только действующей компании"},
	},
}
