// Регрессия на реальных файлах репозитория: примеры из examples/ и боевой
// dsms.yml обязаны парситься и конвертироваться без ошибок. Дешёвая страховка
// от «работает на синтетических тестах, падает на настоящем стеке».
package stackspec

import (
	"os"
	"testing"
)

func TestRepoExamplesConvert(t *testing.T) {
	files := []string{
		"../../../examples/demo-stack.yml",
		"../../../examples/dsms-test-stack.yml",
		"../../../dsms.yml",
	}
	env := map[string]string{
		"REGISTRY": "myuser", "TAG": "v1",
		"APP_MSG": "hi", "DB_PASSWORD": "p",
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			// Тест запущен вне репозитория — проверять нечего.
			t.Skipf("файл %s недоступен: %v", f, err)
		}
		p, err := Load("demo", data, env)
		if err != nil {
			t.Fatalf("%s: Load: %v", f, err)
		}
		c, err := Convert("demo", p, env)
		if err != nil {
			t.Fatalf("%s: Convert: %v", f, err)
		}
		if len(c.Services) == 0 {
			t.Fatalf("%s: не разобрано ни одного сервиса", f)
		}
	}
}

// TestOwnStackKeepsMountsAndSecrets: боевой dsms.yml — самый честный пример.
// Агенту нужны 4 bind-монтирования (/proc, /sys, /, docker.sock) и секрет
// токена; потеря любого из них ломает продукт.
func TestOwnStackKeepsMountsAndSecrets(t *testing.T) {
	data, err := os.ReadFile("../../../dsms.yml")
	if err != nil {
		t.Skipf("dsms.yml недоступен: %v", err)
	}
	p, err := Load("dsms", data, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, err := Convert("dsms", p, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	agent := findService(c, "dsms_agent")
	if agent == nil {
		t.Fatal("сервис dsms_agent не найден")
	}
	cs := agent.Spec.TaskTemplate.ContainerSpec
	if len(cs.Mounts) != 4 {
		t.Fatalf("агенту нужны 4 монтирования хоста, получено %d: %+v", len(cs.Mounts), cs.Mounts)
	}
	if len(cs.Secrets) != 1 || cs.Secrets[0].SecretName != "agent_token" {
		t.Fatalf("секрет agent_token потерян: %+v", cs.Secrets)
	}
	// Секрет external — панель его не создаёт, только ссылается.
	for _, s := range c.Secrets {
		if !s.External {
			t.Fatalf("секреты dsms.yml объявлены external, получено: %+v", s)
		}
	}
}
