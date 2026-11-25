// Честный расчёт «сколько места освободит очистка образов» (ТЗ 3.11.5).
//
// Образы Docker состоят из слоёв, и слои общие: десять версий одного
// приложения делят базовый слой (ОС, JRE). Если сложить полные размеры
// неиспользуемых образов, общий слой посчитается десять раз — оценка
// «можно освободить 6 ГБ» при реальных 500 МБ. К тому же общий слой вообще
// не освободится, пока им пользуется хотя бы один работающий образ.
//
// Здесь слои считаются по одному разу. Что известно о слоях из Docker API:
//   - ImageInspect → RootFS.Layers: точные идентификаторы слоёв по порядку;
//   - ImageHistory → размеры шагов сборки по порядку (у шагов без слоя — 0).
//
// Какой именно слой нулевого размера соответствует какому шагу, API не
// говорит. Обходим это без догадок: два образа, совпадающие в первых k слоях,
// совпадают и в начале последовательности ненулевых размеров — общая часть
// равна сумме совпадающего начала (не длиннее k). Результат сверяется с
// цифрой самого Docker (SharedSize); не сошлось — оценка помечается
// приблизительной.
package dockerops

// imageLayers — что агент знает о слоях одного образа.
type imageLayers struct {
	// diffIDs — идентификаторы слоёв от базового к верхнему.
	diffIDs []string
	// sizes — размеры слоёв ненулевого размера, байты, в том же порядке.
	sizes []int64
}

// total — размер образа: сумма всех его слоёв, байты.
func (l imageLayers) total() int64 {
	var sum int64
	for _, s := range l.sizes {
		sum += s
	}
	return sum
}

// layerNode — узел дерева слоёв: «образ до этого слоя включительно».
// Общие слои нескольких образов — это общие узлы от корня.
type layerNode struct {
	children map[string]*layerNode
	depth    int // сколько слоёв от корня до узла включительно
	images   int // сколько образов проходит через узел
	// seq — размеры ненулевых слоёв первого образа, прошедшего через узел;
	// common — сколько первых размеров совпадает у всех образов узла.
	seq    []int64
	common int
	// keptMark / seenMark — номера проходов, в которых узел помечен как
	// нужный остающимся образам / уже посчитанный (см. freedSizes).
	keptMark int
	seenMark int
}

// sized — сколько слоёв ненулевого размера на пути от корня до узла.
// Не больше глубины узла и не больше общего начала последовательностей
// размеров всех образов узла.
func (n *layerNode) sized() int {
	return min(n.depth, n.common)
}

// cum — суммарный размер слоёв от корня до узла включительно, байты.
func (n *layerNode) cum() int64 {
	var sum int64
	for _, s := range n.seq[:n.sized()] {
		sum += s
	}
	return sum
}

// commonPrefix — длина совпадающего начала двух последовательностей.
func commonPrefix(a, b []int64) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// layerTree — дерево слоёв всех образов ноды.
type layerTree struct {
	root   *layerNode
	images map[string]imageLayers // ID образа → его слои
	pass   int                    // счётчик проходов freedSizes
}

// newLayerTree строит дерево по слоям образов (ID образа → слои).
func newLayerTree(images map[string]imageLayers) *layerTree {
	t := &layerTree{root: &layerNode{children: map[string]*layerNode{}}, images: images}
	for _, l := range images {
		node := t.root
		for i, id := range l.diffIDs {
			child := node.children[id]
			if child == nil {
				child = &layerNode{children: map[string]*layerNode{}, depth: i + 1, seq: l.sizes, common: len(l.sizes)}
				node.children[id] = child
			} else {
				child.common = min(child.common, commonPrefix(child.seq, l.sizes))
			}
			child.images++
			node = child
		}
	}
	return t
}

// path возвращает узлы образа от первого слоя к последнему.
func (t *layerTree) path(l imageLayers) []*layerNode {
	out := make([]*layerNode, 0, len(l.diffIDs))
	node := t.root
	for _, id := range l.diffIDs {
		node = node.children[id]
		if node == nil {
			break // образа нет в дереве — не должно случаться
		}
		out = append(out, node)
	}
	return out
}

// sharedSize — сколько байт образа лежит в слоях, общих с другими образами.
// Docker считает то же число сам (image.Summary.SharedSize) — по нему
// проверяется, что дерево построено верно.
func (t *layerTree) sharedSize(id string) int64 {
	var deepest *layerNode
	for _, n := range t.path(t.images[id]) {
		if n.images > 1 {
			deepest = n // общие слои всегда идут подряд от корня
		}
	}
	if deepest == nil {
		return 0
	}
	return deepest.cum()
}

// freedSizes — размеры слоёв, которые исчезнут с диска, если удалить образы
// removable, а остальные оставить. Каждый слой — один раз; слои, нужные
// остающимся образам, не входят.
//
// У каждого удаляемого образа освобождается «хвост» — слои глубже самого
// глубокого узла, который нужен остающемуся образу или уже посчитан.
// Пример: работает A = база 300 МБ + 50 МБ; удаляем B = та же база + 70 МБ
// и C = та же база + 70 МБ (как у B) + 5 МБ → освободятся слои 70 и 5 МБ.
func (t *layerTree) freedSizes(removable func(id string) bool) []int64 {
	t.pass++
	pass := t.pass
	for id, l := range t.images {
		if removable(id) {
			continue
		}
		for _, n := range t.path(l) {
			n.keptMark = pass
		}
	}
	var out []int64
	for id, l := range t.images {
		if !removable(id) {
			continue
		}
		covered := 0 // сколько первых ненулевых слоёв образа остаётся или уже посчитано
		for _, n := range t.path(l) {
			if n.keptMark == pass || n.seenMark == pass {
				covered = n.sized()
			}
			n.seenMark = pass
		}
		if covered < len(l.sizes) {
			out = append(out, l.sizes[covered:]...)
		}
	}
	return out
}

// reclaimable — сколько байт занимают слои, нужные только образам removable.
func (t *layerTree) reclaimable(removable func(id string) bool) int64 {
	var sum int64
	for _, s := range t.freedSizes(removable) {
		sum += s
	}
	return sum
}
