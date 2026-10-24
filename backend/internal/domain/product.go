package domain

import "math"

// Product — кредитный продукт. Ставки и лимиты синтетические (для демо).
type Product struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Rate        float64  `json:"rate"` // годовая ставка, %
	MinAmount   int64    `json:"min_amount"`
	MaxAmount   int64    `json:"max_amount"`
	MinTerm     int      `json:"min_term"`
	MaxTerm     int      `json:"max_term"`
	DefaultAmt  int64    `json:"default_amount"`
	DefaultTerm int      `json:"default_term"`
	Purposes    []string `json:"purposes"`
	Badge       string   `json:"badge,omitempty"`
	// Express — одноуровневый продукт: решение по быстрой анкете сразу окончательное, без полной анкеты.
	Express bool `json:"express"`
	// DefaultPayment — платёж при сумме и сроке по умолчанию (считает сервер).
	DefaultPayment int64 `json:"default_payment"`
}

var catalog = []Product{
	{
		ID: "business-dev", Name: "Кредит на развитие", Badge: "Популярный",
		Description: "На запуск направления, ремонт, маркетинг и любые цели бизнеса",
		Rate:        19.9, MinAmount: 300_000, MaxAmount: 30_000_000, MinTerm: 6, MaxTerm: 60,
		DefaultAmt: 3_000_000, DefaultTerm: 24,
		Purposes: []string{"Развитие бизнеса", "Ремонт помещения", "Маркетинг", "Рефинансирование"},
	},
	{
		ID: "working-capital", Name: "Оборотный кредит",
		Description: "Закупка товара и сырья, кассовые разрывы, зарплата",
		Rate:        21.5, MinAmount: 100_000, MaxAmount: 10_000_000, MinTerm: 3, MaxTerm: 24,
		DefaultAmt: 1_000_000, DefaultTerm: 12,
		Purposes: []string{"Закупка товара", "Кассовый разрыв", "Выплата зарплаты", "Налоги"},
	},
	{
		ID: "equipment", Name: "На оборудование и транспорт",
		Description: "Покупка оборудования, спецтехники и коммерческого транспорта",
		Rate:        18.5, MinAmount: 500_000, MaxAmount: 50_000_000, MinTerm: 12, MaxTerm: 84,
		DefaultAmt: 5_000_000, DefaultTerm: 36,
		Purposes: []string{"Оборудование", "Спецтехника", "Коммерческий транспорт"},
	},
	{
		ID: "overdraft", Name: "Овердрафт",
		Description: "Деньги на счёте, когда своих не хватает. Платите только за использованное",
		Rate:        23.0, MinAmount: 50_000, MaxAmount: 5_000_000, MinTerm: 12, MaxTerm: 12,
		DefaultAmt: 500_000, DefaultTerm: 12,
		Purposes: []string{"Кассовые разрывы"},
		Badge:    "Решение за минуту",
		Express:  true,
	},
}

func Products() []Product {
	out := make([]Product, len(catalog))
	for i, p := range catalog {
		p.DefaultPayment = NewQuote(p.DefaultAmt, p.Rate, p.DefaultTerm).MonthlyPayment
		out[i] = p
	}
	return out
}

func FindProduct(id string) (Product, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Product{}, false
}

// AnnuityPayment — ежемесячный аннуитетный платёж, округлённый до рубля.
func AnnuityPayment(amount int64, annualRate float64, months int) int64 {
	if months <= 0 {
		return 0
	}
	r := annualRate / 100 / 12
	if r == 0 {
		return int64(math.Ceil(float64(amount) / float64(months)))
	}
	p := float64(amount) * r / (1 - math.Pow(1+r, -float64(months)))
	return int64(math.Round(p))
}

// MaxAmountForPayment — какую сумму можно взять при заданном платеже (обратная аннуитету).
func MaxAmountForPayment(payment int64, annualRate float64, months int) int64 {
	if payment <= 0 || months <= 0 {
		return 0
	}
	r := annualRate / 100 / 12
	if r == 0 {
		return payment * int64(months)
	}
	return int64(float64(payment) * (1 - math.Pow(1+r, -float64(months))) / r)
}

// Calculate — расчёт для калькулятора с проверкой лимитов продукта.
func Calculate(p Product, amount int64, term int) (Quote, error) {
	if amount < p.MinAmount || amount > p.MaxAmount {
		return Quote{}, &ValidationError{"amount", "сумма вне диапазона продукта"}
	}
	if term < p.MinTerm || term > p.MaxTerm {
		return Quote{}, &ValidationError{"term_months", "срок вне диапазона продукта"}
	}
	return NewQuote(amount, p.Rate, term), nil
}
