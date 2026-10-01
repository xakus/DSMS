// Secrets и configs стека (ТЗ 3.14.5, 3.14.8): ссылки сервисов и объекты,
// которые панель должна создать перед деплоем.
//
// Ограничение по месту: панель работает с загруженным текстом файла, диска
// пользователя у неё нет. Поэтому `file:` внутри secrets/configs — ошибка
// с понятным объяснением, а не тихий пропуск: сервис, стартовавший без
// секрета, всё равно упадёт, только позже и невнятнее.
package stackspec

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/swarm"
)

// defaultFileMode — режим файла секрета/конфига по умолчанию (как в Docker).
const defaultFileMode = 0o444

// FileObject — secret или config стека: что панель должна найти или создать.
type FileObject struct {
	Key      string            // ключ в compose-файле
	Name     string            // реальное имя в Docker
	Data     []byte            // содержимое; пусто для external
	External bool              // external — панель не создаёт, только ссылается
	Labels   map[string]string // labels стека (для создаваемых объектов)
}

// fileObjects превращает секцию secrets/configs в список объектов.
// content берётся из `content:` или из переменной окружения (`environment:`).
func fileObjects(stack string, objs map[string]types.FileObjectConfig,
	env map[string]string, kind string) ([]FileObject, error) {

	keys := make([]string, 0, len(objs))
	for k := range objs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]FileObject, 0, len(keys))
	for _, key := range keys {
		o := objs[key]
		fo := FileObject{Key: key, Name: objectName(stack, key, o), External: bool(o.External)}
		if fo.External {
			out = append(out, fo)
			continue
		}

		switch {
		case o.Content != "":
			fo.Data = []byte(o.Content)
		case o.Environment != "":
			v, ok := env[o.Environment]
			if !ok {
				return nil, fmt.Errorf("%s %q: переменная окружения %q не задана",
					kind, key, o.Environment)
			}
			fo.Data = []byte(v)
		case o.File != "":
			return nil, fmt.Errorf("%s %q задан через file: %s — панель не читает файлы с диска; "+
				"создайте объект заранее и укажите external: true, либо перенесите значение в content/environment",
				kind, key, o.File)
		default:
			return nil, fmt.Errorf("%s %q: не задано ни content, ни environment, ни external", kind, key)
		}

		fo.Labels = map[string]string{StackLabel: stack}
		for k, v := range o.Labels {
			fo.Labels[k] = v
		}
		out = append(out, fo)
	}
	return out, nil
}

// objectName — реальное имя объекта в Docker. compose-go обычно уже проставил
// его (`<stack>_<key>`, для external — ключ); правило дублируется на случай,
// когда имя пустое.
func objectName(stack, key string, o types.FileObjectConfig) string {
	if o.Name != "" {
		return o.Name
	}
	if o.External {
		return key
	}
	return stack + "_" + key
}

// secretRefs строит ссылки сервиса на секреты. ID проставляет оркестратор —
// на этапе конвертации кластера ещё не видно.
func secretRefs(svcName string, refs []types.ServiceSecretConfig,
	names map[string]string) ([]*swarm.SecretReference, error) {

	out := make([]*swarm.SecretReference, 0, len(refs))
	for _, r := range refs {
		real, ok := names[r.Source]
		if !ok {
			return nil, fmt.Errorf("сервис %q ссылается на неизвестный secret %q — "+
				"добавьте его в секцию secrets", svcName, r.Source)
		}
		out = append(out, &swarm.SecretReference{
			SecretName: real,
			File: &swarm.SecretReferenceFileTarget{
				Name: targetOrSource(r.Target, r.Source),
				UID:  uidOrZero(r.UID),
				GID:  uidOrZero(r.GID),
				Mode: fileMode(modeOrDefault(r.Mode)),
			},
		})
	}
	return out, nil
}

// configRefs — то же для configs.
func configRefs(svcName string, refs []types.ServiceConfigObjConfig,
	names map[string]string) ([]*swarm.ConfigReference, error) {

	out := make([]*swarm.ConfigReference, 0, len(refs))
	for _, r := range refs {
		real, ok := names[r.Source]
		if !ok {
			return nil, fmt.Errorf("сервис %q ссылается на неизвестный config %q — "+
				"добавьте его в секцию configs", svcName, r.Source)
		}
		out = append(out, &swarm.ConfigReference{
			ConfigName: real,
			File: &swarm.ConfigReferenceFileTarget{
				Name: targetOrSource(r.Target, r.Source),
				UID:  uidOrZero(r.UID),
				GID:  uidOrZero(r.GID),
				Mode: fileMode(modeOrDefault(r.Mode)),
			},
		})
	}
	return out, nil
}

// secretsDir — стандартный каталог секретов внутри контейнера.
const secretsDir = "/run/secrets/"

// targetOrSource: без target файл монтируется под именем источника.
// compose-go нормализует target секрета в абсолютный путь /run/secrets/<имя>,
// а Swarm ждёт короткое имя относительно этого каталога — приводим обратно,
// как это делает docker stack deploy. Абсолютный путь вне /run/secrets
// оставляем как есть: Docker умеет монтировать секрет по такому пути.
func targetOrSource(target, source string) string {
	if target == "" {
		return source
	}
	return strings.TrimPrefix(target, secretsDir)
}

func uidOrZero(v string) string {
	if v == "" {
		return "0"
	}
	return v
}

func modeOrDefault(m *types.FileMode) int64 {
	if m == nil {
		return defaultFileMode
	}
	return int64(*m)
}

// fileMode переводит числовой режим в os.FileMode. Значение приходит из
// compose-файла, поэтому мусор (отрицательное, больше 07777) заменяется
// дефолтом, а не превращается в случайные права на секрет.
func fileMode(m int64) os.FileMode {
	if m < 0 || m > 0o7777 {
		return os.FileMode(defaultFileMode)
	}
	return os.FileMode(uint32(m))
}

// objectNameMap — карта «ключ compose → реальное имя» по списку объектов.
func objectNameMap(objs []FileObject) map[string]string {
	out := make(map[string]string, len(objs))
	for _, o := range objs {
		out[o.Key] = o.Name
	}
	return out
}
