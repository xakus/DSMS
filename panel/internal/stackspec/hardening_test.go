// Тесты этапа 10 (закрытие дыр FR-14): тома (3.14.9), secrets/configs
// (3.14.5), предупреждения о непереносимых полях (3.14.10).
//
// Смысл этих тестов — не «покрытие», а страховка от молчаливой потери
// конфигурации: сервис без тома теряет данные, без секрета — не стартует.
package stackspec

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
)

// build — парсинг + конвертация одного файла с заданным env.
func build(t *testing.T, stack, yaml string, env map[string]string) *Converted {
	t.Helper()
	p, err := Load(stack, []byte(yaml), env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, err := Convert(stack, p, env)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return c
}

// buildErr — ожидаем ошибку конвертации и возвращаем её текст.
func buildErr(t *testing.T, stack, yaml string, env map[string]string) string {
	t.Helper()
	p, err := Load(stack, []byte(yaml), env)
	if err != nil {
		return err.Error()
	}
	if _, err = Convert(stack, p, env); err == nil {
		t.Fatal("ожидалась ошибка конвертации, её нет")
	}
	return err.Error()
}

// mountByTarget ищет монтирование по целевому пути.
func mountByTarget(ms []mount.Mount, target string) *mount.Mount {
	for i := range ms {
		if ms[i].Target == target {
			return &ms[i]
		}
	}
	return nil
}

const yamlVolumes = `
services:
  db:
    image: postgres:17
    volumes:
      - pgdata:/var/lib/postgresql/data
      - shared:/mnt/shared
      - /etc/localtime:/etc/localtime:ro
      - type: tmpfs
        target: /tmp/cache
        tmpfs:
          size: 67108864
volumes:
  pgdata:
    driver: local
    driver_opts:
      type: none
  shared:
    external: true
`

// TestVolumesConverted — главный тест этапа: тома доезжают до спеки,
// именованный получает префикс стека, external — нет, bind остаётся путём.
func TestVolumesConverted(t *testing.T) {
	c := build(t, "app", yamlVolumes, nil)
	db := findService(c, "app_db")
	if db == nil {
		t.Fatal("сервис app_db не найден")
	}
	ms := db.Spec.TaskTemplate.ContainerSpec.Mounts
	if len(ms) != 4 {
		t.Fatalf("mounts: want 4, got %d (%+v)", len(ms), ms)
	}

	// Именованный том стека: <stack>_<key>, тип volume.
	pg := mountByTarget(ms, "/var/lib/postgresql/data")
	if pg == nil || pg.Type != mount.TypeVolume || pg.Source != "app_pgdata" {
		t.Fatalf("именованный том неверен: %+v", pg)
	}
	// Драйвер и его опции обязаны уехать в спеку — Docker создаст том сам.
	if pg.VolumeOptions == nil || pg.VolumeOptions.DriverConfig == nil ||
		pg.VolumeOptions.DriverConfig.Name != "local" ||
		pg.VolumeOptions.DriverConfig.Options["type"] != "none" {
		t.Fatalf("driver/driver_opts тома потеряны: %+v", pg.VolumeOptions)
	}
	// Label стека на томе.
	if pg.VolumeOptions.Labels[StackLabel] != "app" {
		t.Fatalf("label стека на томе потерян: %+v", pg.VolumeOptions.Labels)
	}

	// external-том: имя как есть, без префикса стека.
	sh := mountByTarget(ms, "/mnt/shared")
	if sh == nil || sh.Source != "shared" {
		t.Fatalf("external-том должен остаться 'shared': %+v", sh)
	}

	// bind: путь хоста + read-only.
	lt := mountByTarget(ms, "/etc/localtime")
	if lt == nil || lt.Type != mount.TypeBind || lt.Source != "/etc/localtime" || !lt.ReadOnly {
		t.Fatalf("bind-монтирование неверно: %+v", lt)
	}

	// tmpfs с размером.
	tm := mountByTarget(ms, "/tmp/cache")
	if tm == nil || tm.Type != mount.TypeTmpfs || tm.TmpfsOptions == nil ||
		tm.TmpfsOptions.SizeBytes != 67108864 {
		t.Fatalf("tmpfs неверен: %+v", tm)
	}

	// bind обязан дать предупреждение о привязке к ноде.
	if !hasWarning(c.Warnings, "db", "volumes") {
		t.Fatalf("bind без предупреждения о привязке к ноде: %+v", c.Warnings)
	}
}

const yamlSecrets = `
services:
  api:
    image: alpine
    secrets:
      - jwt
      - source: db_pass
        target: postgres_password
        mode: 0400
    configs:
      - source: nginx_conf
        target: /etc/nginx/nginx.conf
secrets:
  jwt:
    environment: JWT_SECRET
  db_pass:
    external: true
configs:
  nginx_conf:
    content: "worker_processes 1;"
`

// TestSecretsConfigsConverted — ссылки сервиса и объекты для создания.
func TestSecretsConfigsConverted(t *testing.T) {
	c := build(t, "app", yamlSecrets, map[string]string{"JWT_SECRET": "s3cret"})

	// Объекты стека: jwt создаётся из env, db_pass — external.
	var jwt, dbPass *FileObject
	for i := range c.Secrets {
		switch c.Secrets[i].Key {
		case "jwt":
			jwt = &c.Secrets[i]
		case "db_pass":
			dbPass = &c.Secrets[i]
		}
	}
	if jwt == nil || string(jwt.Data) != "s3cret" || jwt.Name != "app_jwt" {
		t.Fatalf("secret из environment неверен: %+v", jwt)
	}
	if jwt.Labels[StackLabel] != "app" {
		t.Fatalf("label стека на секрете потерян: %+v", jwt.Labels)
	}
	if dbPass == nil || !dbPass.External || dbPass.Name != "db_pass" {
		t.Fatalf("external secret неверен: %+v", dbPass)
	}
	if len(c.Configs) != 1 || string(c.Configs[0].Data) != "worker_processes 1;" {
		t.Fatalf("config из content неверен: %+v", c.Configs)
	}

	// Ссылки сервиса: имя файла, режим, реальное имя объекта.
	api := findService(c, "app_api")
	if api == nil {
		t.Fatal("сервис app_api не найден")
	}
	refs := api.Spec.TaskTemplate.ContainerSpec.Secrets
	if len(refs) != 2 {
		t.Fatalf("secret refs: want 2, got %d", len(refs))
	}
	// Без target файл монтируется под именем источника.
	if refs[0].SecretName != "app_jwt" || refs[0].File.Name != "jwt" || refs[0].File.Mode != 0o444 {
		t.Fatalf("ссылка на jwt неверна: %+v %+v", refs[0], refs[0].File)
	}
	if refs[1].SecretName != "db_pass" || refs[1].File.Name != "postgres_password" ||
		refs[1].File.Mode != 0o400 {
		t.Fatalf("ссылка на db_pass неверна: %+v %+v", refs[1], refs[1].File)
	}
	cfgRefs := api.Spec.TaskTemplate.ContainerSpec.Configs
	if len(cfgRefs) != 1 || cfgRefs[0].ConfigName != "app_nginx_conf" ||
		cfgRefs[0].File.Name != "/etc/nginx/nginx.conf" {
		t.Fatalf("ссылка на config неверна: %+v", cfgRefs)
	}
}

// TestSecretFromFileRejected: `file:` панель прочитать не может — нужна
// понятная ошибка, а не сервис, падающий в рантайме.
func TestSecretFromFileRejected(t *testing.T) {
	yaml := `
services:
  api:
    image: alpine
    secrets: [ jwt ]
secrets:
  jwt:
    file: ./jwt.txt
`
	msg := buildErr(t, "app", yaml, nil)
	if !strings.Contains(msg, "external") {
		t.Fatalf("ошибка должна подсказывать про external: %q", msg)
	}
}

// TestUnknownSecretRejected: ссылка на необъявленный секрет — ошибка.
func TestUnknownSecretRejected(t *testing.T) {
	yaml := `
services:
  api:
    image: alpine
    secrets: [ ghost ]
`
	msg := buildErr(t, "app", yaml, nil)
	if !strings.Contains(msg, "ghost") {
		t.Fatalf("в ошибке должно быть имя секрета: %q", msg)
	}
}

const yamlUnsupported = `
services:
  web:
    image: nginx
    build: ./web
    depends_on: [ db ]
    container_name: my-web
    restart: always
    privileged: true
    mem_limit: 512m
  db:
    image: postgres:17
`

// TestWarningsForUnsupported: непереносимые поля видны пользователю до деплоя.
func TestWarningsForUnsupported(t *testing.T) {
	c := build(t, "app", yamlUnsupported, nil)
	for _, field := range []string{"build", "depends_on", "container_name", "restart", "privileged", "mem_limit"} {
		if !hasWarning(c.Warnings, "web", field) {
			t.Fatalf("нет предупреждения о поле %q: %+v", field, c.Warnings)
		}
	}
	// Чистый сервис предупреждений не даёт.
	for _, w := range c.Warnings {
		if w.Service == "db" {
			t.Fatalf("лишнее предупреждение для db: %+v", w)
		}
	}
}

// TestNoWarningsForCleanStack: корректный swarm-файл проходит без шума —
// иначе предупреждения перестанут читать.
func TestNoWarningsForCleanStack(t *testing.T) {
	c := build(t, "shad", yamlSample, map[string]string{"REGISTRY": "x"})
	if len(c.Warnings) != 0 {
		t.Fatalf("чистый стек не должен давать предупреждений: %+v", c.Warnings)
	}
}

// TestServiceFieldsConverted: поля, которые раньше терялись (A-07).
func TestServiceFieldsConverted(t *testing.T) {
	yaml := `
services:
  app:
    image: alpine
    user: "1000:1000"
    working_dir: /srv
    hostname: app-host
    read_only: true
    init: true
    stop_grace_period: 45s
    stop_signal: SIGTERM
    sysctls:
      net.core.somaxconn: "1024"
    cap_add: [ NET_ADMIN ]
    dns: [ 1.1.1.1 ]
    logging:
      driver: json-file
      options:
        max-size: 10m
    deploy:
      endpoint_mode: dnsrr
      placement:
        max_replicas_per_node: 2
        preferences:
          - spread: node.labels.zone
`
	c := build(t, "app", yaml, nil)
	s := findService(c, "app_app")
	if s == nil {
		t.Fatal("сервис не найден")
	}
	cs := s.Spec.TaskTemplate.ContainerSpec
	if cs.User != "1000:1000" || cs.Dir != "/srv" || cs.Hostname != "app-host" {
		t.Fatalf("user/working_dir/hostname потеряны: %+v", cs)
	}
	if !cs.ReadOnly || cs.Init == nil || !*cs.Init {
		t.Fatalf("read_only/init потеряны: %+v", cs)
	}
	if cs.StopGracePeriod == nil || cs.StopGracePeriod.Seconds() != 45 || cs.StopSignal != "SIGTERM" {
		t.Fatalf("stop_grace_period/stop_signal потеряны: %+v", cs)
	}
	if cs.Sysctls["net.core.somaxconn"] != "1024" || len(cs.CapabilityAdd) != 1 {
		t.Fatalf("sysctls/cap_add потеряны: %+v", cs)
	}
	if cs.DNSConfig == nil || len(cs.DNSConfig.Nameservers) != 1 {
		t.Fatalf("dns потерян: %+v", cs.DNSConfig)
	}
	if d := s.Spec.TaskTemplate.LogDriver; d == nil || d.Name != "json-file" ||
		d.Options["max-size"] != "10m" {
		t.Fatalf("logging потерян: %+v", s.Spec.TaskTemplate.LogDriver)
	}
	if s.Spec.EndpointSpec == nil || string(s.Spec.EndpointSpec.Mode) != "dnsrr" {
		t.Fatalf("endpoint_mode потерян: %+v", s.Spec.EndpointSpec)
	}
	pl := s.Spec.TaskTemplate.Placement
	if pl == nil || pl.MaxReplicas != 2 || len(pl.Preferences) != 1 ||
		pl.Preferences[0].Spread.SpreadDescriptor != "node.labels.zone" {
		t.Fatalf("placement preferences/max_replicas потеряны: %+v", pl)
	}
}

// TestNetworkLabelsAndOpts: сеть стека получает label namespace и driver_opts.
func TestNetworkLabelsAndOpts(t *testing.T) {
	yaml := `
services:
  a:
    image: alpine
    networks: [ internal, outside ]
networks:
  internal:
    driver: overlay
    internal: true
    driver_opts:
      encrypted: "true"
  outside:
    external: true
    name: shared-net
`
	c := build(t, "app", yaml, nil)
	var internal, outside *NetworkSpec
	for i := range c.Networks {
		switch c.Networks[i].Name {
		case "app_internal":
			internal = &c.Networks[i]
		case "shared-net":
			outside = &c.Networks[i]
		}
	}
	if internal == nil || internal.Labels[StackLabel] != "app" {
		t.Fatalf("label стека на сети потерян: %+v", internal)
	}
	if !internal.Internal || internal.Options["encrypted"] != "true" {
		t.Fatalf("internal/driver_opts сети потеряны: %+v", internal)
	}
	if outside == nil || !outside.External {
		t.Fatalf("external-сеть должна попасть в список как external: %+v", c.Networks)
	}
	// Сервис привязан к реальным именам сетей.
	s := findService(c, "app_a")
	targets := map[string]bool{}
	for _, n := range s.Spec.TaskTemplate.Networks {
		targets[n.Target] = true
	}
	if !targets["app_internal"] || !targets["shared-net"] {
		t.Fatalf("привязка сервиса к сетям неверна: %+v", targets)
	}
}

// hasWarning — есть ли предупреждение по сервису и полю.
func hasWarning(ws []Warning, service, field string) bool {
	for _, w := range ws {
		if w.Service == service && w.Field == field {
			return true
		}
	}
	return false
}
