package domain

import (
	"fmt"
	"math"
	"strings"
)

// Quote — расчёт условий кредита. ЕДИНСТВЕННЫЙ источник платежа, переплаты и графика:
// фронтенд не считает деньги сам, а получает Quote из API (/calc, заявка, решение).
type Quote struct {
	Amount         int64   `json:"amount"`
	TermMonths     int     `json:"term_months"`
	Rate           float64 `json:"rate"`
	MonthlyPayment int64   `json:"monthly_payment"`
	Total          int64   `json:"total"`
	Overpayment    int64   `json:"overpayment"`
}

// Installment — строка графика платежей.
type Installment struct {
	N         int   `json:"n"`
	Payment   int64 `json:"payment"`
	Principal int64 `json:"principal"`
	Interest  int64 `json:"interest"`
	Rest      int64 `json:"rest"`
}

// Schedule — аннуитетный график с округлением до рубля. Последний платёж
// корректируется так, чтобы долг закрылся ровно в ноль.
func Schedule(amount int64, annualRate float64, months int) []Installment {
	if months <= 0 || amount <= 0 {
		return nil
	}
	pay := AnnuityPayment(amount, annualRate, months)
	r := annualRate / 100 / 12
	rest := amount
	out := make([]Installment, 0, months)
	for n := 1; n <= months; n++ {
		interest := int64(math.Round(float64(rest) * r))
		principal := pay - interest
		if n == months || principal > rest {
			principal = rest
		}
		rest -= principal
		out = append(out, Installment{N: n, Payment: principal + interest, Principal: principal, Interest: interest, Rest: rest})
	}
	return out
}

func NewQuote(amount int64, annualRate float64, months int) Quote {
	q := Quote{Amount: amount, TermMonths: months, Rate: annualRate, MonthlyPayment: AnnuityPayment(amount, annualRate, months)}
	for _, i := range Schedule(amount, annualRate, months) {
		q.Total += i.Payment
	}
	q.Overpayment = q.Total - amount
	return q
}

// ---------- форматирование для объяснений клиенту ----------

func Money(v int64) string {
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return b.String() + " ₽"
}

func Months(n int) string {
	m, h := n%10, n%100
	switch {
	case m == 1 && h != 11:
		return fmt.Sprintf("%d месяц", n)
	case m >= 2 && m <= 4 && (h < 10 || h >= 20):
		return fmt.Sprintf("%d месяца", n)
	}
	return fmt.Sprintf("%d месяцев", n)
}

func Years(y float64) string {
	n := int(y)
	m, h := n%10, n%100
	switch {
	case m == 1 && h != 11:
		return fmt.Sprintf("%d год", n)
	case m >= 2 && m <= 4 && (h < 10 || h >= 20):
		return fmt.Sprintf("%d года", n)
	}
	return fmt.Sprintf("%d лет", n)
}

func Rate(r float64) string { return strings.Replace(fmt.Sprintf("%.1f%%", r), ".", ",", 1) }
