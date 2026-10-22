package uniform

import (
	"reflect"
	"testing"
)

func TestBuild_SetsAllUniforms(t *testing.T) {
	b := New()

	got := b.Build(Input{
		Time:    1.5,
		Width:   1280,
		Height:  720,
		Beat:    0.25,
		CursorX: 10,
		CursorY: 20,
		Frame:   42,
		Random:  0.75,
	})

	want := map[string]any{
		"Time":       float32(1.5),
		"Resolution": []float32{1280, 720},
		"Beat":       float32(0.25),
		"Cursor":     []float32{10, 20},
		"Frame":      42,
		"Random":     float32(0.75),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

// 毎フレームの確保を避けるため、map とスライスは同じものを使い回す。
func TestBuild_ReusesMapAndSlices(t *testing.T) {
	b := New()

	m1 := b.Build(Input{Width: 1, Height: 2, CursorX: 3, CursorY: 4})
	res1 := m1["Resolution"].([]float32)
	cur1 := m1["Cursor"].([]float32)

	m2 := b.Build(Input{Width: 5, Height: 6, CursorX: 7, CursorY: 8})

	if reflect.ValueOf(m1).Pointer() != reflect.ValueOf(m2).Pointer() {
		t.Errorf("map が作り直された")
	}
	if &res1[0] != &m2["Resolution"].([]float32)[0] {
		t.Errorf("Resolution のスライスが作り直された")
	}
	if &cur1[0] != &m2["Cursor"].([]float32)[0] {
		t.Errorf("Cursor のスライスが作り直された")
	}
	if res1[0] != 5 || res1[1] != 6 || cur1[0] != 7 || cur1[1] != 8 {
		t.Errorf("値が更新されていない: res=%v cur=%v", res1, cur1)
	}
}

func TestBuild_DoesNotAllocate(t *testing.T) {
	b := New()
	in := Input{Time: 1, Width: 640, Height: 480, Beat: 0.5, Frame: 1, Random: 0.1}
	b.Build(in) // 初回で map のエントリを作る

	allocs := testing.AllocsPerRun(100, func() {
		in.Frame++
		b.Build(in)
	})
	// any への格納で float32 は確保が起こり得るが、map とスライスは作り直さない。
	// Frame（int）・Time・Beat・Random の 4 つの箱詰めを上限とする。
	if allocs > 4 {
		t.Errorf("allocs = %v, want <= 4", allocs)
	}
}
