package integrations

import (
	"context"
	"sort"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

// ESIA — Единая система идентификации и аутентификации (Госуслуги). В MVP — мок: вместо
// OAuth-перехода на gosuslugi.ru — окно с демо-людьми, которые руководят демо-компаниями.
type ESIA interface {
	Persons(ctx context.Context) []ESIAPerson
	Person(ctx context.Context, id string) (ESIAPerson, bool)
}

// ESIAPerson — учётная запись Госуслуг: личность, контакты и компания, которой человек руководит
// (связь берётся из ЕГРЮЛ/ЕГРИП: руководитель ООО или сам ИП).
type ESIAPerson struct {
	ID         string          `json:"id"`
	Phone      string          `json:"-"` // подтверждённый телефон из Госуслуг — по нему находим аккаунт в кабинете
	Identity   domain.Identity `json:"-"`
	CompanyINN string          `json:"company_inn"`
}

// ESIAPersonView — что видно в окне выбора учётной записи: без паспорта, СНИЛС закрыт.
type ESIAPersonView struct {
	ID          string `json:"id"`
	FullName    string `json:"full_name"`
	SNILS       string `json:"snils"`
	CompanyINN  string `json:"company_inn"`
	CompanyName string `json:"company_name"`
	Role        string `json:"role"`
}

var esiaPeople = []ESIAPerson{
	{ID: "esia-1", Phone: "79000000001", CompanyINN: "7701234567", Identity: domain.Identity{
		FullName: "Смирнов Алексей Викторович", BirthDate: "1983-04-17", SNILS: "112-233-445 95",
		Passport: domain.Passport{Series: "4512", Number: "345678", IssuedAt: "2016-05-24", IssuedBy: "ГУ МВД России по г. Москве", DivisionCode: "770-045"}}},
	{ID: "esia-2", Phone: "79000000002", CompanyINN: "2224000012", Identity: domain.Identity{
		FullName: "Кузнецова Мария Олеговна", BirthDate: "1988-11-02", SNILS: "123-456-789 64",
		Passport: domain.Passport{Series: "0115", Number: "112233", IssuedAt: "2018-02-14", IssuedBy: "ГУ МВД России по Алтайскому краю", DivisionCode: "220-001"}}},
	{ID: "esia-3", Phone: "79000000003", CompanyINN: "5401000009", Identity: domain.Identity{
		FullName: "Петров Игорь Сергеевич", BirthDate: "1979-07-30", SNILS: "134-567-891 05",
		Passport: domain.Passport{Series: "5009", Number: "654321", IssuedAt: "2014-09-10", IssuedBy: "ГУ МВД России по Новосибирской области", DivisionCode: "540-003"}}},
	{ID: "esia-4", Phone: "79000000004", CompanyINN: "6600000000", Identity: domain.Identity{
		FullName: "Орлов Олег Петрович", BirthDate: "1975-01-21", SNILS: "145-678-912 46",
		Passport: domain.Passport{Series: "6505", Number: "778899", IssuedAt: "2020-03-03", IssuedBy: "ГУ МВД России по Свердловской области", DivisionCode: "660-002"}}},
	{ID: "esia-5", Phone: "79000000006", CompanyINN: "222400000012", Identity: domain.Identity{
		FullName: "Иванов Иван Иванович", BirthDate: "1995-06-12", SNILS: "156-789-123 87",
		Passport: domain.Passport{Series: "0119", Number: "246810", IssuedAt: "2015-07-01", IssuedBy: "ГУ МВД России по Алтайскому краю", DivisionCode: "220-004"}}},
}

type MockESIA struct{}

func (MockESIA) Persons(context.Context) []ESIAPerson {
	return append([]ESIAPerson(nil), esiaPeople...)
}

func (MockESIA) Person(_ context.Context, id string) (ESIAPerson, bool) {
	for _, p := range esiaPeople {
		if p.ID == id {
			p.Identity.Source = "Госуслуги (ЕСИА, демо)"
			return p, true
		}
	}
	return ESIAPerson{}, false
}

// MaskSNILS закрывает середину СНИЛС: «112-•••-••• 95».
func MaskSNILS(s string) string {
	if len(s) != 14 {
		return "•••"
	}
	return s[:3] + "-•••-••• " + s[12:]
}

// DemoCompany — компания из демо-списка для поля ИНН на 1-м уровне анкеты.
type DemoCompany struct {
	INN      string `json:"inn"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Director string `json:"director"`
	Hint     string `json:"hint"` // чем компания интересна для демонстрации
}

var demoHints = map[string]string{
	"7701234567":   "одобрение",
	"2224000012":   "другие условия при большой сумме",
	"5401000009":   "просрочки в БКИ — видно на полном скоринге",
	"6600000000":   "ликвидирована — отказ сразу",
	"222400000012": "ИП, бизнесу меньше года",
}

// DemoCompanies — компании с заранее известным поведением (те же, что в моке реестра).
func DemoCompanies() []DemoCompany {
	out := make([]DemoCompany, 0, len(fixtures))
	for inn, c := range fixtures {
		out = append(out, DemoCompany{INN: inn, Name: c.Name, Type: c.Type, Director: c.Director, Hint: demoHints[inn]})
	}
	order := map[string]int{"7701234567": 1, "2224000012": 2, "5401000009": 3, "6600000000": 4, "222400000012": 5}
	sort.Slice(out, func(i, j int) bool { return order[out[i].INN] < order[out[j].INN] })
	return out
}
