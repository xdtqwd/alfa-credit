// Package integrations — адаптеры внешних источников. В MVP это локальные моки,
// воспроизводящие смысл интеграций (ЕГРЮЛ/ЕГРИП, «Прозрачный бизнес», БКИ) без сетевых вызовов.
package integrations

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

// Registry — ЕГРЮЛ/ЕГРИП + «Прозрачный бизнес» ФНС.
type Registry interface {
	Lookup(ctx context.Context, inn string) (*domain.Company, error)
}

// CreditBureau — бюро кредитных историй.
type CreditBureau interface {
	Report(ctx context.Context, inn string) (*domain.BureauReport, error)
}

// Демо-ИНН с заранее известным поведением (см. README → «Тестовые сценарии»).
var fixtures = map[string]domain.Company{
	"7701234567": {
		Type: "ООО", Name: "ООО «Северный ветер»", OGRN: "1157746123456",
		OKVED: "46.90", OKVEDName: "Торговля оптовая неспециализированная",
		Address: "г. Москва, ул. Лесная, д. 5", Director: "Смирнов Алексей Викторович",
		RegDate: "2015-03-12", Status: "active", RevenueLastYear: 84_000_000,
	},
	"2224000012": {
		Type: "ООО", Name: "ООО «Алтайские сыроварни»", OGRN: "1182225001234",
		OKVED: "10.51", OKVEDName: "Производство молочной продукции",
		Address: "г. Барнаул, пр-т Ленина, д. 100", Director: "Кузнецова Мария Олеговна",
		RegDate: "2018-07-01", Status: "active", RevenueLastYear: 18_000_000,
	},
	"5401000009": {
		Type: "ООО", Name: "ООО «Сибирский кран»", OGRN: "1105401009999",
		OKVED: "43.99", OKVEDName: "Специализированные строительные работы",
		Address: "г. Новосибирск, ул. Мира, д. 12", Director: "Петров Игорь Сергеевич",
		RegDate: "2010-09-20", Status: "active", RevenueLastYear: 30_000_000,
	},
	"6600000000": {
		Type: "ООО", Name: "ООО «Закрытый горизонт»", OGRN: "1126600000000",
		OKVED: "47.19", OKVEDName: "Розничная торговля",
		Address: "г. Екатеринбург, ул. Малышева, д. 1", Director: "Орлов Олег Петрович",
		RegDate: "2012-01-15", Status: "liquidated", RevenueLastYear: 0,
	},
	"222400000012": {
		Type: "ИП", Name: "ИП Иванов Иван Иванович", OGRN: "318222500012345",
		OKVED: "62.01", OKVEDName: "Разработка компьютерного программного обеспечения",
		Address: "г. Барнаул", Director: "Иванов Иван Иванович",
		RegDate: "2025-11-01", Status: "active", RevenueLastYear: 2_400_000,
	},
}

var (
	genNames  = []string{"Вектор", "Гранит", "Березка", "Меридиан", "Атлант", "Полюс", "Сфера", "Ладья", "Кедр", "Магистраль"}
	genOKVED  = [][2]string{{"47.11", "Розничная торговля в неспециализированных магазинах"}, {"56.10", "Деятельность ресторанов"}, {"49.41", "Грузовые перевозки"}, {"41.20", "Строительство зданий"}, {"62.01", "Разработка ПО"}}
	genCities = []string{"г. Барнаул", "г. Новосибирск", "г. Томск", "г. Казань", "г. Екатеринбург"}
	genPeople = []string{"Волков Дмитрий Андреевич", "Соколова Анна Павловна", "Морозов Сергей Ильич", "Лебедева Ольга Николаевна"}
)

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

type MockRegistry struct{}

func (MockRegistry) Lookup(_ context.Context, inn string) (*domain.Company, error) {
	if !domain.ValidINN(inn) {
		return nil, &domain.ValidationError{Field: "inn", Message: "ИНН должен содержать 10 или 12 цифр"}
	}
	if c, ok := fixtures[inn]; ok {
		c.INN = inn
		c.Source = "ЕГРЮЛ/ЕГРИП (мок)"
		return &c, nil
	}
	if strings.HasPrefix(inn, "0000") {
		return nil, domain.ErrCompanyNotFound
	}
	h := hash(inn)
	ok := genOKVED[h%uint32(len(genOKVED))]
	c := &domain.Company{
		INN: inn, OKVED: ok[0], OKVEDName: ok[1],
		Address:         genCities[h%uint32(len(genCities))],
		Director:        genPeople[h%uint32(len(genPeople))],
		RegDate:         fmt.Sprintf("%d-%02d-15", 2008+h%16, 1+h%12),
		Status:          "active",
		RevenueLastYear: int64(10+h%190) * 1_000_000,
		Source:          "ЕГРЮЛ/ЕГРИП (мок)",
	}
	if len(inn) == 12 {
		c.Type = "ИП"
		c.Name = "ИП " + c.Director
		c.OGRN = "3" + inn[:12] + "45"
	} else {
		c.Type = "ООО"
		c.Name = "ООО «" + genNames[h%uint32(len(genNames))] + "»"
		c.OGRN = "1" + inn + "12"
	}
	return c, nil
}

type MockBureau struct{}

// Report: ИНН, оканчивающийся на 9, имеет просрочки — для демонстрации контрпредложения/отказа.
func (MockBureau) Report(_ context.Context, inn string) (*domain.BureauReport, error) {
	if strings.HasSuffix(inn, "9") {
		return &domain.BureauReport{Score: 540, ActiveLoans: 4, HasOverdue: true, MaxOverdueDays: 45, Source: "БКИ (мок)"}, nil
	}
	h := hash("bki" + inn)
	return &domain.BureauReport{Score: 700 + int(h%120), ActiveLoans: int(h % 3), Source: "БКИ (мок)"}, nil
}
