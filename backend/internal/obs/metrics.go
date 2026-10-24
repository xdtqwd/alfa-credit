package obs

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Registry — минимальный реестр метрик в формате Prometheus (text exposition 0.0.4).
// Счётчики и гистограммы с метками; гауджи — через функции, которые вызываются при сборе.
type Registry struct {
	mu     sync.Mutex
	counts map[string]*counterVec
	hists  map[string]*histVec
	gauges []gaugeFunc
}

type gaugeFunc struct {
	name, help string
	fn         func() []Sample
}

// Sample — одно значение гауджа с метками.
type Sample struct {
	Labels map[string]string
	Value  float64
}

func NewRegistry() *Registry {
	return &Registry{counts: map[string]*counterVec{}, hists: map[string]*histVec{}}
}

// Default — общий реестр процесса.
var Default = NewRegistry()

type counterVec struct {
	help string
	mu   sync.Mutex
	m    map[string]*atomic.Int64 // ключ — отсортированные метки
	lbl  map[string]map[string]string
}

// Inc увеличивает счётчик name с метками (пары ключ-значение).
func (r *Registry) Inc(name, help string, labels ...string) { r.Add(name, help, 1, labels...) }

func (r *Registry) Add(name, help string, v int64, labels ...string) {
	r.mu.Lock()
	c, ok := r.counts[name]
	if !ok {
		c = &counterVec{help: help, m: map[string]*atomic.Int64{}, lbl: map[string]map[string]string{}}
		r.counts[name] = c
	}
	r.mu.Unlock()
	key, lm := labelKey(labels)
	c.mu.Lock()
	n, ok := c.m[key]
	if !ok {
		n = &atomic.Int64{}
		c.m[key], c.lbl[key] = n, lm
	}
	c.mu.Unlock()
	n.Add(v)
}

var defBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

type hist struct {
	counts []int64
	sum    float64
	n      int64
	lbl    map[string]string
}

type histVec struct {
	help    string
	buckets []float64
	mu      sync.Mutex
	m       map[string]*hist
}

// Observe добавляет наблюдение (секунды) в гистограмму.
func (r *Registry) Observe(name, help string, v float64, labels ...string) {
	r.mu.Lock()
	h, ok := r.hists[name]
	if !ok {
		h = &histVec{help: help, buckets: defBuckets, m: map[string]*hist{}}
		r.hists[name] = h
	}
	r.mu.Unlock()
	key, lm := labelKey(labels)
	h.mu.Lock()
	defer h.mu.Unlock()
	x, ok := h.m[key]
	if !ok {
		x = &hist{counts: make([]int64, len(h.buckets)), lbl: lm}
		h.m[key] = x
	}
	for i, b := range h.buckets {
		if v <= b {
			x.counts[i]++
		}
	}
	x.sum += v
	x.n++
}

// Gauge регистрирует функцию, значения которой снимаются при каждом сборе.
func (r *Registry) Gauge(name, help string, fn func() []Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, gaugeFunc{name, help, fn})
}

// CounterValue — текущее значение счётчика (для тестов и /bank/stats).
func (r *Registry) CounterValue(name string, labels ...string) int64 {
	r.mu.Lock()
	c, ok := r.counts[name]
	r.mu.Unlock()
	if !ok {
		return 0
	}
	key, _ := labelKey(labels)
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.m[key]; ok {
		return n.Load()
	}
	return 0
}

// CounterTotal — сумма счётчика по всем сочетаниям меток (для кабинета банка).
func (r *Registry) CounterTotal(name string) int64 {
	r.mu.Lock()
	c, ok := r.counts[name]
	r.mu.Unlock()
	if !ok {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for _, n := range c.m {
		total += n.Load()
	}
	return total
}

// WriteText пишет все метрики в формате Prometheus.
func (r *Registry) WriteText(w io.Writer) {
	// карты имён читаются только под r.mu: параллельно Add/Observe могут регистрировать новые метрики
	r.mu.Lock()
	cn := sortedKeys(r.counts)
	hn := sortedKeys(r.hists)
	counts := make([]*counterVec, len(cn))
	for i, name := range cn {
		counts[i] = r.counts[name]
	}
	hists := make([]*histVec, len(hn))
	for i, name := range hn {
		hists[i] = r.hists[name]
	}
	gs := append([]gaugeFunc(nil), r.gauges...)
	r.mu.Unlock()

	for i, name := range cn {
		c := counts[i]
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", name, c.help, name)
		c.mu.Lock()
		for _, k := range sortedKeys(c.m) {
			fmt.Fprintf(w, "%s%s %d\n", name, fmtLabels(c.lbl[k], ""), c.m[k].Load())
		}
		c.mu.Unlock()
	}
	for i, name := range hn {
		h := hists[i]
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", name, h.help, name)
		h.mu.Lock()
		for _, k := range sortedKeys(h.m) {
			x := h.m[k]
			for i, b := range h.buckets {
				fmt.Fprintf(w, "%s_bucket%s %d\n", name, fmtLabels(x.lbl, fmt.Sprintf(`le="%g"`, b)), x.counts[i])
			}
			fmt.Fprintf(w, "%s_bucket%s %d\n", name, fmtLabels(x.lbl, `le="+Inf"`), x.n)
			fmt.Fprintf(w, "%s_sum%s %g\n%s_count%s %d\n", name, fmtLabels(x.lbl, ""), x.sum, name, fmtLabels(x.lbl, ""), x.n)
		}
		h.mu.Unlock()
	}
	for _, g := range gs {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
		for _, s := range g.fn() {
			v := s.Value
			if math.IsNaN(v) {
				continue
			}
			fmt.Fprintf(w, "%s%s %g\n", g.name, fmtLabels(s.Labels, ""), v)
		}
	}
}

func labelKey(kv []string) (string, map[string]string) {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	keys := sortedKeys(m)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + m[k] + ";")
	}
	return b.String(), m
}

func fmtLabels(m map[string]string, extra string) string {
	parts := make([]string, 0, len(m)+1)
	for _, k := range sortedKeys(m) {
		v := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(m[k])
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
