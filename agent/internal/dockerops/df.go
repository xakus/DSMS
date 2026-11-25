// Disk usage ноды — аналог `docker system df` (ТЗ 3.11.1) с оценкой того,
// сколько места освободит каждая кнопка очистки (ТЗ 3.11.5).
//
// Главное правило: оценка «можно освободить» обещает ровно то, что потом
// сделает Prune. Раньше оценка считала больше, чем очистка удаляла:
// складывала полные размеры образов с общими слоями и считала именованные
// тома, которые очистка не трогает.
package dockerops

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
)

// DF — disk usage одной ноды для ответа панели (3.11.1).
type DF struct {
	Images     DFSection `json:"images"`
	Containers DFSection `json:"containers"`
	Volumes    DFSection `json:"volumes"`
	BuildCache DFSection `json:"build_cache"`
	// Total — сколько освободит «Очистить всё»: контейнеры, образы, кэш
	// сборки и анонимные тома по очереди (см. PruneOrder).
	Total DFTotal `json:"total"`
}

// DFTotal — оценка полной очистки ноды, байты.
type DFTotal struct {
	// Reclaimable — образы чистятся только без тега.
	Reclaimable int64 `json:"reclaimable"`
	// ReclaimableAll — чистятся все образы без работающих контейнеров.
	ReclaimableAll int64 `json:"reclaimable_all"`
	// Approx — оценка образов приблизительная («не меньше»).
	Approx bool `json:"approx,omitempty"`
}

// DFSection — одна категория disk usage. Все размеры — в байтах.
type DFSection struct {
	Count int `json:"count"`
	// Size — сколько категория занимает на диске (общие слои — один раз).
	Size int64 `json:"size"`
	// Prunable — сколько объектов удалит кнопка «Очистить». Кнопка нужна и
	// при Reclaimable = 0: остановленный контейнер без данных всё равно
	// держит свой образ, а образ — слои в кэше сборки.
	Prunable int `json:"prunable"`
	// Reclaimable — сколько места освободит кнопка «Очистить» сразу, с
	// настройками по умолчанию. Для образов — только образы без тега.
	Reclaimable int64 `json:"reclaimable"`
	// PrunableAll / ReclaimableAll — только образы: то же при флажке
	// «все неиспользуемые образы».
	PrunableAll    int   `json:"prunable_all,omitempty"`
	ReclaimableAll int64 `json:"reclaimable_all,omitempty"`
	// Later / LaterAll — только образы: сколько ещё места образов освободит
	// «Очистить всё». Это слои, которые держат остановленные контейнеры и
	// кэш сборки: одной очисткой образов их с диска не убрать.
	Later    int64 `json:"later,omitempty"`
	LaterAll int64 `json:"later_all,omitempty"`
	// Approx — только образы: слои не удалось сверить с Docker, поэтому
	// оценка — нижняя граница («не меньше»), а не точное число.
	Approx bool `json:"approx,omitempty"`
	// Kept — только тома: размер неиспользуемых именованных томов. Очистка их
	// не удаляет (в них данные сервисов), поэтому в Reclaimable их нет.
	Kept int64 `json:"kept,omitempty"`
}

// Цели очистки (поле targets запроса prune).
const (
	TargetContainers = "containers"
	TargetImages     = "images"
	TargetBuildCache = "build-cache"
	TargetVolumes    = "volumes"
)

// PruneOrder — порядок полной очистки ноды. Он важен: остановленный
// контейнер держит свой образ, а слои образа, собранного на этой ноде,
// держит кэш сборки. В другом порядке очистка освободила бы меньше.
var PruneOrder = []string{TargetContainers, TargetImages, TargetBuildCache, TargetVolumes}

// Признаки объектов Docker, от которых зависит, что удалит очистка.
const (
	// noneTag — так старые версии API обозначают отсутствие тега у образа.
	noneTag = "<none>:<none>"
	// labelAnonymousVolume — Docker ставит эту метку анонимным томам
	// (созданным без имени). Только их удаляет `docker volume prune`.
	labelAnonymousVolume = "com.docker.volume.anonymous"
	// Записи кэша сборки, которые `docker builder prune` без --all не трогает.
	cacheTypeInternal = "internal"
	cacheTypeFrontend = "frontend"
)

// DiskUsage собирает disk usage ноды с оценкой очистки.
func (o *Ops) DiskUsage(ctx context.Context) (*DF, error) {
	du, err := o.c.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return nil, err
	}
	df := &DF{
		Containers: containersDF(du.Containers),
		Volumes:    volumesDF(du.Volumes),
		BuildCache: buildCacheDF(du.BuildCache),
	}
	var est imageEstimate
	df.Images, est = o.imagesDF(ctx, du)
	// Полная очистка: всё, что освобождают категории, плюс слои образов,
	// которые станут свободны после контейнеров и кэша сборки.
	rest := df.Containers.Reclaimable + df.Volumes.Reclaimable + df.BuildCache.Reclaimable
	df.Total = DFTotal{
		Reclaimable:    rest + est.dangling.now + est.dangling.later,
		ReclaimableAll: rest + est.all.now + est.all.later,
		Approx:         est.approx,
	}
	return df, nil
}

// isActive — контейнер считается занятым: очистка его не удалит.
// Так же делит контейнеры `docker system df`.
func isActive(state container.ContainerState) bool {
	switch state {
	case container.StateRunning, container.StatePaused, container.StateRestarting:
		return true
	}
	return false
}

// containersDF: место — записываемые слои контейнеров; освободится то, что
// занято остановленными.
func containersDF(list []*container.Summary) DFSection {
	var sec DFSection
	for _, c := range list {
		if c == nil {
			continue
		}
		sec.Count++
		sec.Size += c.SizeRw
		if !isActive(c.State) {
			sec.Prunable++
			sec.Reclaimable += c.SizeRw
		}
	}
	return sec
}

// volumesDF: освободится место неиспользуемых анонимных томов; неиспользуемые
// именованные показываем отдельно (Kept) — очистка их не удаляет.
func volumesDF(list []*volume.Volume) DFSection {
	var sec DFSection
	for _, v := range list {
		if v == nil {
			continue
		}
		sec.Count++
		// Размер -1 — Docker не смог его посчитать (например, чужой драйвер).
		if v.UsageData == nil || v.UsageData.Size < 0 {
			continue
		}
		sec.Size += v.UsageData.Size
		if v.UsageData.RefCount > 0 {
			continue
		}
		if _, anonymous := v.Labels[labelAnonymousVolume]; anonymous {
			sec.Prunable++
			sec.Reclaimable += v.UsageData.Size
		} else {
			sec.Kept += v.UsageData.Size
		}
	}
	return sec
}

// buildCacheDF: записи с пометкой Shared делят место с образами — в размер
// кэша не входят (иначе одни и те же байты попали бы и в образы, и в кэш).
// Освободится то, что не используется и не служебное.
func buildCacheDF(list []*build.CacheRecord) DFSection {
	var sec DFSection
	for _, b := range list {
		if b == nil {
			continue
		}
		sec.Count++
		if b.Shared {
			continue
		}
		sec.Size += b.Size
		if !b.InUse && b.Type != cacheTypeInternal && b.Type != cacheTypeFrontend {
			sec.Prunable++
			sec.Reclaimable += b.Size
		}
	}
	return sec
}

// imageInfo — что нужно знать об образе, чтобы оценить очистку.
type imageInfo struct {
	id string
	// size и shared — цифры Docker: размер образа и сколько из него лежит в
	// слоях, общих с другими образами (-1 — Docker не посчитал).
	size, shared int64
	tagged       bool // есть тег: очистка по умолчанию такой образ не удалит
	used         bool // есть контейнеры (любые): образ удалить нельзя
	active       bool // есть работающие контейнеры
	layers       imageLayers
	hasLayers    bool // слои удалось прочитать
}

// pruneGain — что даст удаление набора образов.
type pruneGain struct {
	images int // сколько образов будет удалено
	// now — байты, которые освободятся сразу после очистки образов.
	now int64
	// later — байты образов, которые освободятся только при полной очистке:
	// их слои держат остановленные контейнеры и кэш сборки.
	later int64
}

// imageEstimate — оценка очистки образов.
type imageEstimate struct {
	dangling pruneGain // образы без тега
	all      pruneGain // все неиспользуемые образы (флажок в UI)
	approx   bool      // байты — нижняя граница, а не точное число
}

// hasTag — у образа есть настоящий тег (не заглушка <none>).
func hasTag(repoTags []string) bool {
	for _, t := range repoTags {
		if t != "" && t != noneTag {
			return true
		}
	}
	return false
}

// sharedCacheSizes — размеры записей кэша сборки, которые делят место со
// слоями образов (Shared): размер → сколько таких записей.
//
// Слой образа, собранного на этой ноде, принадлежит и образу, и кэшу сборки.
// Удаление образа такой слой с диска не убирает — он остаётся в кэше до его
// очистки. Идентификатора слоя у записи кэша в API нет, зато размер записи
// байт в байт равен размеру слоя — по нему и сопоставляем.
func sharedCacheSizes(list []*build.CacheRecord) map[int64]int {
	out := map[int64]int{}
	for _, b := range list {
		if b != nil && b.Shared && b.Size > 0 {
			out[b.Size]++
		}
	}
	return out
}

// splitByCache делит освобождаемые слои на «уйдут с диска сразу» и
// «останутся в кэше сборки». cache — копия sharedCacheSizes: каждая запись
// кэша удерживает один слой.
func splitByCache(freed []int64, cache map[int64]int) (now, pinned int64) {
	left := make(map[int64]int, len(cache))
	for size, n := range cache {
		left[size] = n
	}
	for _, size := range freed {
		if left[size] > 0 {
			left[size]--
			pinned += size
		} else {
			now += size
		}
	}
	return now, pinned
}

// estimateImages считает, что даст удаление образов. Освобождаются слои,
// нужные только удаляемым образам: общий слой считается один раз, а слой,
// нужный остающемуся образу, не считается вовсе.
//
// Пример: три версии приложения, у всех общая база 300 МБ и свой слой 50 МБ;
// работает только последняя. Удаление двух старых освободит 100 МБ (а не
// 700 МБ, как дала бы сумма размеров образов): база нужна работающей версии.
//
// cache — sharedCacheSizes: слои, которые держит ещё и кэш сборки.
func estimateImages(images []imageInfo, cache map[int64]int) imageEstimate {
	layers := make(map[string]imageLayers, len(images))
	byID := make(map[string]imageInfo, len(images))
	for _, img := range images {
		byID[img.id] = img
		if img.hasLayers {
			layers[img.id] = img.layers
		}
	}
	tree := newLayerTree(layers)

	// Сверка с цифрами Docker: размер образа и общая с другими часть.
	exact := true
	for _, img := range images {
		switch {
		case !img.hasLayers,
			len(img.layers.sizes) > len(img.layers.diffIDs),
			img.layers.total() != img.size,
			img.shared >= 0 && tree.sharedSize(img.id) != img.shared:
			exact = false
		}
	}

	// Гарантированный минимум по цифрам самого Docker: у удаляемого образа
	// точно освободятся слои, которых нет больше ни у кого.
	floor := func(pick func(id string) bool) int64 {
		var sum int64
		for _, img := range images {
			if pick(img.id) && img.shared >= 0 && img.size > img.shared {
				sum += img.size - img.shared
			}
		}
		return sum
	}

	// gain считает набор образов: prunable — удалит очистка образов сейчас,
	// idle — удалит полная очистка (после остановленных контейнеров).
	gain := func(prunable, idle func(id string) bool) pruneGain {
		var g pruneGain
		for _, img := range images {
			if prunable(img.id) {
				g.images++
			}
		}
		g.now, _ = splitByCache(tree.freedSizes(prunable), cache)
		full := tree.reclaimable(idle)
		if !exact {
			g.now = min(g.now, floor(prunable))
			full = min(full, floor(idle))
		}
		if full > g.now {
			g.later = full - g.now
		}
		return g
	}

	unused := func(id string) bool { return !byID[id].used }
	untagged := func(id string) bool { return !byID[id].tagged }
	idle := func(id string) bool { return !byID[id].active }
	return imageEstimate{
		dangling: gain(
			func(id string) bool { return unused(id) && untagged(id) },
			func(id string) bool { return idle(id) && untagged(id) },
		),
		all:    gain(unused, idle),
		approx: !exact,
	}
}

// imagesDF: размер — слои образов на диске (каждый один раз), освободится —
// по дереву слоёв (см. layers.go).
func (o *Ops) imagesDF(ctx context.Context, du types.DiskUsage) (DFSection, imageEstimate) {
	// Какие образы заняты контейнерами: любыми и работающими.
	usedAny := map[string]bool{}
	usedActive := map[string]bool{}
	for _, c := range du.Containers {
		if c == nil {
			continue
		}
		usedAny[c.ImageID] = true
		if isActive(c.State) {
			usedActive[c.ImageID] = true
		}
	}

	infos := make([]imageInfo, 0, len(du.Images))
	alive := make(map[string]bool, len(du.Images))
	for _, img := range du.Images {
		if img == nil {
			continue
		}
		alive[img.ID] = true
		infos = append(infos, o.imageInfoOf(ctx, img, usedAny[img.ID], usedActive[img.ID]))
	}
	o.forgetLayers(alive)

	est := estimateImages(infos, sharedCacheSizes(du.BuildCache))
	return DFSection{
		Count:          len(infos),
		Size:           du.LayersSize,
		Prunable:       est.dangling.images,
		Reclaimable:    est.dangling.now,
		PrunableAll:    est.all.images,
		ReclaimableAll: est.all.now,
		Later:          est.dangling.later,
		LaterAll:       est.all.later,
		Approx:         est.approx,
	}, est
}

// imageInfoOf собирает сведения об образе. inList/activeInList — нашёлся ли
// образ среди контейнеров ноды и есть ли среди них работающие.
func (o *Ops) imageInfoOf(ctx context.Context, img *image.Summary, inList, activeInList bool) imageInfo {
	info := imageInfo{
		id:     img.ID,
		size:   img.Size,
		shared: img.SharedSize,
		tagged: hasTag(img.RepoTags),
		used:   img.Containers > 0 || inList,
		active: activeInList,
	}
	// Docker говорит, что контейнеры есть, а в списке их не видно (ID образа
	// записаны по-разному) — считаем образ занятым работающим контейнером:
	// лучше пообещать меньше, чем больше.
	if img.Containers > 0 && !inList {
		info.active = true
	}
	if l, err := o.layersOf(ctx, img.ID); err == nil {
		info.layers, info.hasLayers = l, true
	}
	return info
}

// layersOf возвращает слои образа: идентификаторы и размеры. Образ
// неизменяем, поэтому два запроса к Docker делаются один раз на образ.
func (o *Ops) layersOf(ctx context.Context, id string) (imageLayers, error) {
	o.layerMu.Lock()
	cached, ok := o.layerCache[id]
	o.layerMu.Unlock()
	if ok {
		return cached, nil
	}

	insp, err := o.c.ImageInspect(ctx, id)
	if err != nil {
		return imageLayers{}, err
	}
	hist, err := o.c.ImageHistory(ctx, id)
	if err != nil {
		return imageLayers{}, err
	}
	l := imageLayers{diffIDs: insp.RootFS.Layers}
	// История идёт от нового шага к старому; слои — от базового к верхнему.
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Size > 0 {
			l.sizes = append(l.sizes, hist[i].Size)
		}
	}

	o.layerMu.Lock()
	o.layerCache[id] = l
	o.layerMu.Unlock()
	return l, nil
}

// forgetLayers чистит кэш слоёв от образов, которых больше нет (alive — ID
// существующих). Без этого кэш рос бы с каждой новой версией образа.
func (o *Ops) forgetLayers(alive map[string]bool) {
	o.layerMu.Lock()
	defer o.layerMu.Unlock()
	for id := range o.layerCache {
		if !alive[id] {
			delete(o.layerCache, id)
		}
	}
}
