// Package stackspec парсит compose/stack YAML и конвертирует его в спецификации
// Docker Swarm (FR-14). Пакет чистый: на вход — текст YAML и env, на выход —
// swarm.ServiceSpec и описания сетей; никакой зависимости от Docker API или БД,
// чтобы конвертацию можно было тестировать в отрыве от кластера.
package stackspec

import (
	"context"
	"fmt"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
)

// Load парсит compose YAML с интерполяцией переменных окружения и возвращает
// готовый проект. name задаёт имя стека (project name), env — значения для
// подстановки ${VAR} / ${VAR:-default}.
func Load(name string, yaml []byte, env map[string]string) (*types.Project, error) {
	m := types.Mapping{}
	for k, v := range env {
		m[k] = v
	}
	details := types.ConfigDetails{
		WorkingDir:  ".",
		ConfigFiles: []types.ConfigFile{{Filename: "stack.yml", Content: yaml}},
		Environment: m,
	}
	// SkipConsistencyCheck — не роняем парсинг на «мягких» несоответствиях
	// (например ссылка на external-ресурс): валидацию делаем сами по месту.
	p, err := loader.LoadWithContext(context.Background(), details, func(o *loader.Options) {
		o.SetProjectName(name, true)
		o.SkipConsistencyCheck = true
	})
	if err != nil {
		return nil, fmt.Errorf("разбор compose: %w", err)
	}
	return p, nil
}

// ParseEnv разбирает текст .env-файла (строки KEY=value) и мёржит поверх него
// ручные пары из UI (manual перекрывает .env). Результат — карта для Load.
func ParseEnv(dotenvText string, manual map[string]string) (map[string]string, error) {
	out := map[string]string{}
	if dotenvText != "" {
		parsed, err := dotenv.Parse(strings.NewReader(dotenvText))
		if err != nil {
			return nil, fmt.Errorf("разбор .env: %w", err)
		}
		for k, v := range parsed {
			out[k] = v
		}
	}
	for k, v := range manual {
		out[k] = v
	}
	return out, nil
}
