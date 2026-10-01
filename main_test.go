package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDrainEvents(t *testing.T) {
	t.Run("空のチャネルならブロックせずに空を返す", func(t *testing.T) {
		ch := make(chan string, 4)
		if got := drainEvents(ch, 16); len(got) != 0 {
			t.Fatalf("got %v, want empty", got)
		}
	})

	t.Run("溜まったイベントを順にすべて取り出す", func(t *testing.T) {
		ch := make(chan string, 4)
		ch <- "a"
		ch <- "b"
		ch <- "c"
		got := drainEvents(ch, 16)
		want := []string{"a", "b", "c"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("上限を超えた分は次のフレームに残す", func(t *testing.T) {
		ch := make(chan string, 4)
		ch <- "a"
		ch <- "b"
		ch <- "c"
		got := drainEvents(ch, 2)
		if !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatalf("got %v", got)
		}
		if rest := drainEvents(ch, 2); !reflect.DeepEqual(rest, []string{"c"}) {
			t.Fatalf("rest %v", rest)
		}
	})

	t.Run("閉じたチャネルでは止まる", func(t *testing.T) {
		ch := make(chan string, 1)
		ch <- "a"
		close(ch)
		if got := drainEvents(ch, 16); !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("got %v", got)
		}
	})
}

func TestProcessEvents(t *testing.T) {
	errCompile := errors.New("compile failed")
	errRead := errors.New("read failed")

	t.Run("成功したファイルのエラーだけを消し、他のファイルのエラーは残す", func(t *testing.T) {
		errs := map[string]error{"a.kage": errCompile, "b.kage": errCompile}
		read := func(string) ([]byte, error) { return []byte("src"), nil }
		reload := func(string, []byte) error { return nil }

		processEvents([]string{"a.kage"}, read, reload, errs)

		if _, ok := errs["a.kage"]; ok {
			t.Errorf("a.kage のエラーが残っている")
		}
		if errs["b.kage"] != errCompile {
			t.Errorf("b.kage のエラーが消えた: %v", errs)
		}
	})

	t.Run("コンパイル失敗はファイルごとに記録する", func(t *testing.T) {
		errs := map[string]error{}
		read := func(string) ([]byte, error) { return []byte("src"), nil }
		reload := func(path string, _ []byte) error {
			if path == "bad.kage" {
				return errCompile
			}
			return nil
		}

		processEvents([]string{"ok.kage", "bad.kage"}, read, reload, errs)

		if len(errs) != 1 || errs["bad.kage"] != errCompile {
			t.Fatalf("errs = %v", errs)
		}
	})

	t.Run("読み込み失敗も記録し、Reload は呼ばない", func(t *testing.T) {
		errs := map[string]error{}
		read := func(string) ([]byte, error) { return nil, errRead }
		called := false
		reload := func(string, []byte) error { called = true; return nil }

		processEvents([]string{"x.kage"}, read, reload, errs)

		if called {
			t.Errorf("読み込み失敗時に reload が呼ばれた")
		}
		if !errors.Is(errs["x.kage"], errRead) {
			t.Errorf("errs = %v", errs)
		}
	})
}

func TestErrorLines(t *testing.T) {
	t.Run("エラーが無ければ空", func(t *testing.T) {
		if got := errorLines(nil, 3); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("ファイル名順にすべて出す", func(t *testing.T) {
		errs := map[string]error{
			"b.kage": errors.New("eb"),
			"a.kage": errors.New("ea"),
		}
		got := errorLines(errs, 3)
		want := []string{"ERROR: ea", "ERROR: eb"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("エラー文がパスを含んでもファイル名を二重に付けない", func(t *testing.T) {
		errs := map[string]error{
			"shaders/a.kage": errors.New("shaders/a.kage:3:5: unexpected token"),
		}
		got := errorLines(errs, 3)
		want := []string{"ERROR: shaders/a.kage:3:5: unexpected token"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("上限を超えたら先頭の数件と残り件数", func(t *testing.T) {
		errs := map[string]error{
			"a": errors.New("1"), "b": errors.New("2"),
			"c": errors.New("3"), "d": errors.New("4"),
		}
		got := errorLines(errs, 2)
		want := []string{"ERROR: 1", "ERROR: 2", "... and 2 more errors"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func TestHUDText(t *testing.T) {
	t.Run("状態行のあとにエラー行が続く", func(t *testing.T) {
		errs := map[string]error{"a.kage": errors.New("boom")}
		got := hudText(120, 4, 0.25, errs)
		want := "BPM: 120.0  FadeBeats: 4.0  Mix: 0.25\nERROR: boom"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("エラーが無ければ状態行だけ", func(t *testing.T) {
		got := hudText(0, 0.5, 0, nil)
		if strings.Contains(got, "\n") {
			t.Fatalf("got %q", got)
		}
	})
}

func TestShaderListLines(t *testing.T) {
	got := shaderListLines([]string{"shaders/a.kage", "shaders/b.kage"})
	want := []string{"shader 1: shaders/a.kage", "shader 2: shaders/b.kage"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
