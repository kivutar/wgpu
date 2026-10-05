// Copyright 2026 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build windows && !(js && wasm)

package dx12

import (
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/gogpu/wgpu/hal/dx12/d3d12"
)

// Capture the native buffer views, rather than just checking the Go cache.
// Look up COM slots by field name to avoid hard-coding vtable indices.
func vertexBufferTestPass(t *testing.T) (*RenderPassEncoder, map[uint32]d3d12.D3D12_VERTEX_BUFFER_VIEW) {
	t.Helper()
	views := make(map[uint32]d3d12.D3D12_VERTEX_BUFFER_VIEW)
	vtableType := reflect.TypeOf(d3d12.ID3D12GraphicsCommandList{}).Field(0).Type.Elem()
	vtable := make([]uintptr, vtableType.Size()/unsafe.Sizeof(uintptr(0)))
	setMethod := func(name string, callback any) {
		field, ok := vtableType.FieldByName(name)
		if !ok {
			t.Fatalf("missing COM method %s", name)
		}
		vtable[field.Offset/unsafe.Sizeof(uintptr(0))] = syscall.NewCallback(callback)
	}
	setMethod("SetPipelineState", func(_, _ uintptr) uintptr { return 0 })
	setMethod("IASetPrimitiveTopology", func(_, _ uintptr) uintptr { return 0 })
	setMethod("IASetVertexBuffers", func(_, slot, count, data uintptr) uintptr {
		var first *d3d12.D3D12_VERTEX_BUFFER_VIEW
		*(*uintptr)(unsafe.Pointer(&first)) = data
		for i, view := range unsafe.Slice(first, int(count)) {
			views[uint32(slot)+uint32(i)] = view
		}
		return 0
	})
	object := &struct{ vtable *uintptr }{&vtable[0]}
	pass := &RenderPassEncoder{encoder: &CommandEncoder{
		cmdList:      (*d3d12.ID3D12GraphicsCommandList)(unsafe.Pointer(object)),
		isRecording:  true,
		stateTracker: newCommandStateTracker(),
	}}
	t.Cleanup(func() {
		runtime.KeepAlive(object)
		runtime.KeepAlive(vtable)
	})
	return pass, views
}

func TestVertexBufferBindingOrderAndPipelineChanges(t *testing.T) {
	for _, bufferFirst := range []bool{true, false} {
		name := "pipeline-first"
		if bufferFirst {
			name = "buffer-first"
		}
		t.Run(name, func(t *testing.T) {
			pass, views := vertexBufferTestPass(t)
			buffer := &Buffer{gpuVA: 0x1000, size: 1024}
			pipeline := &RenderPipeline{vertexStrides: []uint32{32}, samplerRootIndex: -1}
			if bufferFirst {
				pass.SetVertexBuffer(0, buffer, 64)
				pass.SetPipeline(pipeline)
			} else {
				pass.SetPipeline(pipeline)
				pass.SetVertexBuffer(0, buffer, 64)
			}
			check := func(address uint64, size, stride uint32) {
				t.Helper()
				want := d3d12.D3D12_VERTEX_BUFFER_VIEW{BufferLocation: address, SizeInBytes: size, StrideInBytes: stride}
				if got := views[0]; got != want {
					t.Fatalf("native vertex view = %+v, want %+v", got, want)
				}
			}
			check(0x1040, 960, 32)
			pass.SetPipeline(&RenderPipeline{vertexStrides: []uint32{64}, samplerRootIndex: -1})
			check(0x1040, 960, 64)
			pass.SetPipeline(pipeline)
			check(0x1040, 960, 32)
			pass.SetVertexBuffer(0, &Buffer{gpuVA: 0x2000, size: 512}, 128)
			check(0x2080, 384, 32)
			pass.SetPipeline(&RenderPipeline{vertexStrides: []uint32{0}, samplerRootIndex: -1})
			check(0x2080, 384, 0)
		})
	}
}

func TestVertexBufferStridesAcrossUnusedSlots(t *testing.T) {
	pass, views := vertexBufferTestPass(t)
	pass.SetVertexBuffer(0, &Buffer{gpuVA: 0x1000, size: 256}, 0)
	pass.SetVertexBuffer(2, &Buffer{gpuVA: 0x2000, size: 512}, 128)
	pass.SetPipeline(&RenderPipeline{vertexStrides: []uint32{32, 16, 96}, samplerRootIndex: -1})
	if _, bound := views[1]; bound {
		t.Fatal("unbound slot 1 was bound")
	}
	if views[0].StrideInBytes != 32 || views[2].StrideInBytes != 96 {
		t.Fatalf("wrong per-slot strides: %+v", views)
	}
	// A pipeline may temporarily stop using a still-bound instance buffer.
	pass.SetPipeline(&RenderPipeline{vertexStrides: []uint32{64}, samplerRootIndex: -1})
	pass.SetVertexBuffer(2, &Buffer{gpuVA: 0x3000, size: 1024}, 64)
	pass.SetPipeline(&RenderPipeline{vertexStrides: []uint32{32, 16, 48}, samplerRootIndex: -1})
	want := d3d12.D3D12_VERTEX_BUFFER_VIEW{BufferLocation: 0x3040, SizeInBytes: 960, StrideInBytes: 48}
	if views[2] != want {
		t.Fatalf("restored slot 2 = %+v, want %+v", views[2], want)
	}
}
