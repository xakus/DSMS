// Тесты кольцевого буфера live-метрик (разд. 2.2).
//
// Буфер — единственное место, где живут точки за 15 минут: ошибка в
// арифметике кольца даёт либо перепутанный порядок на графике, либо
// «дыру» в данных, которую трудно заметить глазами.
package metrics

import (
	"sync"
	"testing"
	"time"
)

func point(nodeID string, ts int64, cpu float64) Snapshot {
	return Snapshot{NodeID: nodeID, TS: ts, CPU: CPU{TotalPct: cpu}}
}

// TestEmptyBuffer: пустой буфер отвечает «данных нет», а не пустыми точками.
func TestEmptyBuffer(t *testing.T) {
	b := NewClusterBuffer(15*time.Minute, 3*time.Second)

	if _, ok := b.Latest("нет-такой-ноды"); ok {
		t.Fatal("Latest вернул данные для неизвестной ноды")
	}
	if w := b.Window("нет-такой-ноды"); w != nil {
		t.Fatalf("Window должен быть nil: %+v", w)
	}
	if l := b.Latests(); len(l) != 0 {
		t.Fatalf("Latests должен быть пуст: %+v", l)
	}
}

// TestWindowOrder: точки отдаются от старых к новым — так их рисует график.
func TestWindowOrder(t *testing.T) {
	b := NewClusterBuffer(time.Minute, time.Second) // ёмкость 60

	for i := 1; i <= 5; i++ {
		b.Put(point("node-1", int64(i), float64(i*10)))
	}
	window := b.Window("node-1")
	if len(window) != 5 {
		t.Fatalf("ожидалось 5 точек, получено %d", len(window))
	}
	for i, s := range window {
		if s.TS != int64(i+1) {
			t.Fatalf("нарушен порядок точек: %+v", window)
		}
	}
	latest, ok := b.Latest("node-1")
	if !ok || latest.TS != 5 {
		t.Fatalf("последняя точка неверна: %+v", latest)
	}
}

// TestRingEviction: при переполнении вытесняются самые старые точки,
// а порядок остаётся правильным (типичный баг арифметики кольца).
func TestRingEviction(t *testing.T) {
	b := NewClusterBuffer(3*time.Second, time.Second) // ёмкость 3

	for i := 1; i <= 5; i++ {
		b.Put(point("node-1", int64(i), float64(i)))
	}
	window := b.Window("node-1")
	if len(window) != 3 {
		t.Fatalf("окно должно держать 3 точки, получено %d", len(window))
	}
	for i, want := range []int64{3, 4, 5} {
		if window[i].TS != want {
			t.Fatalf("после вытеснения ожидались точки 3,4,5: %+v", window)
		}
	}
}

// TestNodesAreIsolated: метрики одной ноды не попадают в окно другой.
func TestNodesAreIsolated(t *testing.T) {
	b := NewClusterBuffer(time.Minute, time.Second)

	b.Put(point("node-1", 1, 10))
	b.Put(point("node-2", 2, 20))
	b.Put(point("node-2", 3, 30))

	if w := b.Window("node-1"); len(w) != 1 || w[0].CPU.TotalPct != 10 {
		t.Fatalf("окно node-1 испорчено: %+v", w)
	}
	if w := b.Window("node-2"); len(w) != 2 {
		t.Fatalf("окно node-2 испорчено: %+v", w)
	}

	all := b.Latests()
	if len(all) != 2 || all["node-1"].TS != 1 || all["node-2"].TS != 3 {
		t.Fatalf("Latests неверен: %+v", all)
	}
}

// TestMinimalCapacity: шаг больше окна не должен давать кольцо нулевого
// размера (деление на ноль при записи).
func TestMinimalCapacity(t *testing.T) {
	b := NewClusterBuffer(time.Second, time.Minute)

	b.Put(point("node-1", 1, 10))
	b.Put(point("node-1", 2, 20))

	w := b.Window("node-1")
	if len(w) != 1 || w[0].TS != 2 {
		t.Fatalf("минимальное кольцо должно хранить одну последнюю точку: %+v", w)
	}
}

// TestConcurrentAccess: ingest пишет, REST и WS читают одновременно —
// гонок быть не должно (запускать с -race).
func TestConcurrentAccess(t *testing.T) {
	b := NewClusterBuffer(time.Minute, time.Second)
	var wg sync.WaitGroup

	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				b.Put(point("node-1", int64(i), float64(i)))
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				b.Latest("node-1")
				b.Window("node-1")
				b.Latests()
			}
		}()
	}
	wg.Wait()
}
