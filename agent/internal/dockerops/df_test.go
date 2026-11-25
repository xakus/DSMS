package dockerops

import (
	"testing"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
)

const mb = int64(1024 * 1024)

// layersOfSizes собирает слои образа: ids — идентификаторы слоёв по порядку,
// sizes — размеры тех же слоёв (0 — слой нулевого размера, в историю Docker
// он попадает без размера и в последовательность размеров не входит).
func layersOfSizes(ids []string, sizes []int64) imageLayers {
	l := imageLayers{diffIDs: ids}
	for _, s := range sizes {
		if s > 0 {
			l.sizes = append(l.sizes, s)
		}
	}
	return l
}

// img собирает описание образа для оценки; shared — цифра Docker.
func img(id string, l imageLayers, shared int64, tagged, used, active bool) imageInfo {
	return imageInfo{
		id: id, size: l.total(), shared: shared,
		tagged: tagged, used: used, active: active,
		layers: l, hasLayers: true,
	}
}

// noCache — на ноде нет кэша сборки (образы скачаны, а не собраны).
var noCache = map[int64]int{}

// Три версии приложения на общей базе: работает только последняя.
// Сумма размеров неиспользуемых образов — 700 МБ, а освободится 100 МБ:
// база нужна работающей версии. Это и был баг «обещали гигабайты».
func TestEstimateImagesSharedBase(t *testing.T) {
	base := []string{"os", "jre"}
	baseSizes := []int64{100 * mb, 200 * mb}
	v := func(n string, own int64) imageLayers {
		return layersOfSizes(append(append([]string{}, base...), "app-"+n), append(append([]int64{}, baseSizes...), own))
	}
	images := []imageInfo{
		img("v1", v("1", 50*mb), 300*mb, false, false, false),
		img("v2", v("2", 50*mb), 300*mb, false, false, false),
		img("v3", v("3", 60*mb), 300*mb, true, true, true),
	}
	est := estimateImages(images, noCache)
	if est.approx {
		t.Fatal("оценка должна быть точной: цифры Docker сходятся с деревом слоёв")
	}
	if want := 100 * mb; est.all.now != want || est.dangling.now != want {
		t.Fatalf("освободится %d (все) / %d (без тега), ожидалось %d", est.all.now, est.dangling.now, want)
	}
	if est.all.images != 2 || est.dangling.images != 2 {
		t.Fatalf("образов к удалению: %d (все) / %d (без тега), ожидалось 2 и 2", est.all.images, est.dangling.images)
	}
	if est.all.later != 0 {
		t.Fatalf("later = %d: остановленных контейнеров и кэша сборки нет", est.all.later)
	}
}

// Слой, общий только для удаляемых образов, освобождается целиком и
// считается один раз (а не ноль раз и не два).
func TestEstimateImagesLayerSharedOnlyByRemovable(t *testing.T) {
	old1 := layersOfSizes([]string{"os", "deps-old", "a1"}, []int64{100 * mb, 80 * mb, 10 * mb})
	old2 := layersOfSizes([]string{"os", "deps-old", "a2"}, []int64{100 * mb, 80 * mb, 12 * mb})
	cur := layersOfSizes([]string{"os", "deps-new", "a3"}, []int64{100 * mb, 90 * mb, 11 * mb})
	images := []imageInfo{
		img("old1", old1, 180*mb, false, false, false),
		img("old2", old2, 180*mb, false, false, false),
		img("cur", cur, 100*mb, true, true, true),
	}
	est := estimateImages(images, noCache)
	if est.approx {
		t.Fatal("оценка должна быть точной")
	}
	// deps-old (80) + a1 (10) + a2 (12); os нужен работающему образу.
	if want := 102 * mb; est.all.now != want {
		t.Fatalf("освободится %d, ожидалось %d", est.all.now, want)
	}
}

// Образ с тегом без контейнеров: по умолчанию не удаляется, с флажком
// «все неиспользуемые» — удаляется.
func TestEstimateImagesTaggedOnlyWithAll(t *testing.T) {
	images := []imageInfo{
		img("tagged", layersOfSizes([]string{"a"}, []int64{40 * mb}), 0, true, false, false),
		img("untagged", layersOfSizes([]string{"b"}, []int64{30 * mb}), 0, false, false, false),
	}
	est := estimateImages(images, noCache)
	if est.dangling.now != 30*mb || est.all.now != 70*mb {
		t.Fatalf("без тега %d, все %d; ожидалось 30 и 70 МБ", est.dangling.now, est.all.now)
	}
	if est.dangling.images != 1 || est.all.images != 2 {
		t.Fatalf("образов к удалению: %d и %d, ожидалось 1 и 2", est.dangling.images, est.all.images)
	}
}

// Образ держит остановленный контейнер: сейчас не освободится, но
// освободится при полной очистке (после удаления контейнеров).
func TestEstimateImagesHeldByStoppedContainer(t *testing.T) {
	images := []imageInfo{
		img("old", layersOfSizes([]string{"os", "x"}, []int64{100 * mb, 25 * mb}), 100*mb, false, true, false),
		img("cur", layersOfSizes([]string{"os", "y"}, []int64{100 * mb, 30 * mb}), 100*mb, true, true, true),
	}
	est := estimateImages(images, noCache)
	if est.all.now != 0 || est.all.images != 0 {
		t.Fatalf("сейчас: %d байт, %d образов; ожидались нули — образ занят контейнером", est.all.now, est.all.images)
	}
	if est.all.later != 25*mb || est.dangling.later != 25*mb {
		t.Fatalf("later = %d / %d, ожидалось 25 МБ", est.all.later, est.dangling.later)
	}
}

// Образ собран на этой ноде: его слои держит ещё и кэш сборки. Очистка
// образов такой слой с диска не уберёт — он освободится только вместе с
// кэшем. Найдено живой проверкой: Docker удалил образы и отчитался «0 байт».
func TestEstimateImagesPinnedByBuildCache(t *testing.T) {
	built := layersOfSizes([]string{"os", "b"}, []int64{100 * mb, 40 * mb})
	pulled := layersOfSizes([]string{"os", "p"}, []int64{100 * mb, 15 * mb})
	cur := layersOfSizes([]string{"os", "c"}, []int64{100 * mb, 30 * mb})
	images := []imageInfo{
		img("built", built, 100*mb, false, false, false),
		img("pulled", pulled, 100*mb, false, false, false),
		img("cur", cur, 100*mb, true, true, true),
	}
	// В кэше сборки лежат слои собранного образа: база и его собственный слой.
	cache := sharedCacheSizes([]*build.CacheRecord{
		{Shared: true, Size: 100 * mb},
		{Shared: true, Size: 40 * mb},
		{Shared: false, Size: 15 * mb}, // не Shared — со слоями образов не связана
	})
	est := estimateImages(images, cache)
	if est.all.now != 15*mb {
		t.Fatalf("сразу освободится %d, ожидалось 15 МБ (слой скачанного образа)", est.all.now)
	}
	if est.all.later != 40*mb {
		t.Fatalf("later = %d, ожидалось 40 МБ (слой, который держит кэш сборки)", est.all.later)
	}
	if est.all.images != 2 {
		t.Fatalf("образов к удалению %d, ожидалось 2: удаляются оба, хоть один и не освободит место сразу", est.all.images)
	}
}

// Одна запись кэша удерживает один слой: два слоя одного размера и одна
// запись — в кэше остаётся только один из них.
func TestSplitByCacheCountsRecords(t *testing.T) {
	now, pinned := splitByCache([]int64{50, 50, 7}, map[int64]int{50: 1})
	if now != 57 || pinned != 50 {
		t.Fatalf("now %d pinned %d, ожидалось 57 и 50", now, pinned)
	}
}

// Слой нулевого размера (WORKDIR, mkdir) посреди образа: в истории у него
// размер 0, и какой именно это слой — Docker не говорит. Оценка обязана
// остаться точной.
func TestEstimateImagesZeroSizeLayers(t *testing.T) {
	// База: os 100, пустой слой, jre 200. Приложения: пустой слой (WORKDIR) + jar.
	a := layersOfSizes([]string{"os", "mk", "jre", "wd-a", "jar-a"}, []int64{100 * mb, 0, 200 * mb, 0, 50 * mb})
	b := layersOfSizes([]string{"os", "mk", "jre", "wd-b", "jar-b"}, []int64{100 * mb, 0, 200 * mb, 0, 70 * mb})
	images := []imageInfo{
		img("a", a, 300*mb, false, false, false),
		img("b", b, 300*mb, true, true, true),
	}
	est := estimateImages(images, noCache)
	if est.approx {
		t.Fatal("оценка должна быть точной и со слоями нулевого размера")
	}
	if est.all.now != 50*mb {
		t.Fatalf("освободится %d, ожидалось 50 МБ", est.all.now)
	}
}

// Цифры Docker не сходятся с деревом слоёв (другое хранилище образов,
// скрытые промежуточные образы) — оценка помечается приблизительной и не
// превышает гарантированный минимум.
func TestEstimateImagesApproxWhenDockerDisagrees(t *testing.T) {
	a := layersOfSizes([]string{"os", "x"}, []int64{100 * mb, 20 * mb})
	b := layersOfSizes([]string{"os", "y"}, []int64{100 * mb, 30 * mb})
	images := []imageInfo{
		// Docker считает общими 110 МБ (у него есть образ, которого мы не видим).
		img("a", a, 110*mb, false, false, false),
		img("b", b, 100*mb, true, true, true),
	}
	est := estimateImages(images, noCache)
	if !est.approx {
		t.Fatal("ожидалась приблизительная оценка")
	}
	if est.all.now != 10*mb {
		t.Fatalf("освободится %d, ожидался минимум по Docker — 10 МБ", est.all.now)
	}
}

// Слои образа прочитать не удалось — оценка приблизительная, но не падает.
func TestEstimateImagesWithoutLayers(t *testing.T) {
	images := []imageInfo{{id: "x", size: 50 * mb, shared: 10 * mb}}
	est := estimateImages(images, noCache)
	if !est.approx || est.all.now != 0 {
		t.Fatalf("approx=%v now=%d, ожидалось approx и 0 (дерево про образ не знает)", est.approx, est.all.now)
	}
	if est.all.images != 1 {
		t.Fatalf("образов к удалению %d, ожидался 1: удалить его очистка всё равно может", est.all.images)
	}
}

// Порядок обхода образов не должен менять результат (в Go обход map случаен).
func TestLayerTreeIsOrderIndependent(t *testing.T) {
	layers := map[string]imageLayers{
		"a": layersOfSizes([]string{"os", "p", "a"}, []int64{100, 30, 1}),
		"b": layersOfSizes([]string{"os", "p", "b"}, []int64{100, 30, 2}),
		"c": layersOfSizes([]string{"os", "q"}, []int64{100, 7}),
		"d": layersOfSizes([]string{"z"}, []int64{9}),
	}
	const all = 100 + 30 + 1 + 2 + 7 + 9
	const withoutC = 30 + 1 + 2 + 9 // остаётся c: его os и q не освобождаются
	for range 50 {
		tree := newLayerTree(layers)
		if got := tree.reclaimable(func(string) bool { return true }); got != all {
			t.Fatalf("все образы: %d, ожидалось %d", got, all)
		}
		if got := tree.reclaimable(func(id string) bool { return id != "c" }); got != withoutC {
			t.Fatalf("все, кроме c: %d, ожидалось %d", got, withoutC)
		}
	}
}

func TestHasTag(t *testing.T) {
	cases := []struct {
		tags []string
		want bool
	}{
		{nil, false},
		{[]string{"<none>:<none>"}, false},
		{[]string{"shad/gateway:1.4"}, true},
		{[]string{"<none>:<none>", "nginx:1.29"}, true},
	}
	for _, c := range cases {
		if got := hasTag(c.tags); got != c.want {
			t.Errorf("hasTag(%v) = %v, ожидалось %v", c.tags, got, c.want)
		}
	}
}

// Контейнеры: освободится только место остановленных.
func TestContainersDF(t *testing.T) {
	sec := containersDF([]*container.Summary{
		{State: container.StateRunning, SizeRw: 10},
		{State: container.StatePaused, SizeRw: 20},
		{State: container.StateRestarting, SizeRw: 40},
		{State: container.StateExited, SizeRw: 100},
		{State: container.StateCreated, SizeRw: 200},
		nil,
	})
	if sec.Count != 5 || sec.Size != 370 || sec.Reclaimable != 300 || sec.Prunable != 2 {
		t.Fatalf("получено %+v, ожидалось count 5, size 370, reclaimable 300, prunable 2", sec)
	}
}

// Тома: очистка удаляет только неиспользуемые анонимные; именованные с
// данными остановленного сервиса в «можно освободить» не попадают.
func TestVolumesDF(t *testing.T) {
	anon := map[string]string{labelAnonymousVolume: ""}
	sec := volumesDF([]*volume.Volume{
		{Name: "used", UsageData: &volume.UsageData{Size: 500, RefCount: 1}},
		{Name: "anon-free", Labels: anon, UsageData: &volume.UsageData{Size: 70, RefCount: 0}},
		{Name: "anon-used", Labels: anon, UsageData: &volume.UsageData{Size: 30, RefCount: 2}},
		{Name: "pgdata", UsageData: &volume.UsageData{Size: 900, RefCount: 0}},
		{Name: "unknown-size", UsageData: &volume.UsageData{Size: -1, RefCount: 0}},
		{Name: "no-usage"},
	})
	if sec.Count != 6 || sec.Size != 1500 {
		t.Fatalf("count %d size %d, ожидалось 6 и 1500", sec.Count, sec.Size)
	}
	if sec.Reclaimable != 70 || sec.Kept != 900 || sec.Prunable != 1 {
		t.Fatalf("reclaimable %d kept %d prunable %d, ожидалось 70, 900 и 1", sec.Reclaimable, sec.Kept, sec.Prunable)
	}
}

// Кэш сборки: общие с образами записи в размер не входят, служебные и
// используемые не освобождаются.
func TestBuildCacheDF(t *testing.T) {
	sec := buildCacheDF([]*build.CacheRecord{
		{Type: "regular", Size: 100},
		{Type: "regular", Size: 40, InUse: true},
		{Type: "regular", Size: 1000, Shared: true},
		{Type: cacheTypeInternal, Size: 7},
		{Type: cacheTypeFrontend, Size: 5},
		{Type: "exec.cachemount", Size: 60},
	})
	if sec.Count != 6 || sec.Size != 212 || sec.Reclaimable != 160 || sec.Prunable != 2 {
		t.Fatalf("получено %+v, ожидалось count 6, size 212, reclaimable 160, prunable 2", sec)
	}
}

// Полная очистка идёт в одном порядке, как бы ни прислали цели: сначала
// контейнеры (они держат образы), потом образы, кэш сборки, тома.
func TestOrderTargets(t *testing.T) {
	got := orderTargets([]string{TargetVolumes, "bogus", TargetImages, TargetContainers, TargetImages, TargetBuildCache})
	want := []string{TargetContainers, TargetImages, TargetBuildCache, TargetVolumes, "bogus"}
	if len(got) != len(want) {
		t.Fatalf("получено %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("получено %v, ожидалось %v", got, want)
		}
	}
}
