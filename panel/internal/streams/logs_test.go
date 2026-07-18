// Тесты парсинга stdcopy-фреймов и details-меток (FR-05).
package streams

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// frame собирает stdcopy-фрейм: header + payload.
func frame(stream byte, payload string) []byte {
	var h [8]byte
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
	return append(h[:], payload...)
}

// TestReadFrame: демультиплексирование stdout/stderr.
func TestReadFrame(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	buf.Write(frame(1, "hello stdout\n"))
	buf.Write(frame(2, "boom stderr\n"))

	stream, payload, err := readFrame(buf)
	if err != nil || stream != "stdout" || string(payload) != "hello stdout\n" {
		t.Fatalf("frame1: %v %s %q", err, stream, payload)
	}
	stream, payload, err = readFrame(buf)
	if err != nil || stream != "stderr" || string(payload) != "boom stderr\n" {
		t.Fatalf("frame2: %v %s %q", err, stream, payload)
	}
}

// TestParseLine: timestamp + swarm-метки → поля LogLine (3.5.2).
func TestParseLine(t *testing.T) {
	raw := "2026-07-05T12:00:00.123456789Z com.docker.swarm.node.id=n1,com.docker.swarm.service.id=s1,com.docker.swarm.task.id=t1,com.docker.swarm.task.name=web.3.t1 GET /health 200"
	ll := parseLine(raw)
	if ll.TS != "2026-07-05T12:00:00.123456789Z" {
		t.Fatalf("ts: %q", ll.TS)
	}
	if ll.Task != "t1" || ll.Node != "n1" || ll.TaskName != "web.3.t1" {
		t.Fatalf("labels: task=%q node=%q name=%q", ll.Task, ll.Node, ll.TaskName)
	}
	if ll.Line != "GET /health 200" {
		t.Fatalf("line: %q", ll.Line)
	}
}

// TestParseLinePlain: строка без details не теряет содержимое.
func TestParseLinePlain(t *testing.T) {
	raw := "2026-07-05T12:00:00Z plain message without details"
	ll := parseLine(raw)
	if ll.TS == "" || ll.Line != "plain message without details" {
		t.Fatalf("plain: ts=%q line=%q", ll.TS, ll.Line)
	}
	// вообще без timestamp — строка целиком
	ll = parseLine("no timestamp at all")
	if ll.Line != "no timestamp at all" || ll.TS != "" {
		t.Fatalf("no-ts: %+v", ll)
	}
}
