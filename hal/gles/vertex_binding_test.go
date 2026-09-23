// Copyright 2026 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build (windows || linux) && !(js && wasm)

package gles

import (
	"testing"

	"github.com/gogpu/gputypes"
)

func TestVertexBufferBeforePipelineGetsLayout(t *testing.T) {
	encoder := &CommandEncoder{}
	pass := &RenderPassEncoder{encoder: encoder}
	buffer := &Buffer{id: 7}
	pass.SetVertexBuffer(1, buffer, 256)
	pipeline := &RenderPipeline{vertexBuffers: []gputypes.VertexBufferLayout{
		{}, {ArrayStride: 32, StepMode: gputypes.VertexStepModeInstance},
	}}
	pass.SetPipeline(pipeline)
	last := encoder.commands[len(encoder.commands)-1]
	binding, ok := last.(*SetVertexBufferCommand)
	if !ok || binding.buffer != buffer || binding.slot != 1 || binding.offset != 256 || binding.layout != &pipeline.vertexBuffers[1] {
		t.Fatalf("pipeline did not configure the previously bound buffer: %#v", last)
	}
}

func TestPipelineSwitchRestoresVertexLayoutsAndOffsets(t *testing.T) {
	encoder := &CommandEncoder{}
	pass := &RenderPassEncoder{encoder: encoder}
	first := &RenderPipeline{vertexBuffers: []gputypes.VertexBufferLayout{{ArrayStride: 64}, {ArrayStride: 96}}}
	second := &RenderPipeline{vertexBuffers: []gputypes.VertexBufferLayout{{ArrayStride: 32}, {ArrayStride: 16}}}
	pass.SetPipeline(first)
	vertex, instance := &Buffer{id: 3}, &Buffer{id: 4}
	pass.SetVertexBuffer(0, vertex, 64)
	pass.SetVertexBuffer(1, instance, 128)
	pass.SetVertexBuffer(0, vertex, 512)
	start := len(encoder.commands)
	pass.SetPipeline(second)
	bindings := make(map[uint32]*SetVertexBufferCommand)
	for _, cmd := range encoder.commands[start:] {
		if binding, ok := cmd.(*SetVertexBufferCommand); ok {
			bindings[binding.slot] = binding
		}
	}
	for slot, want := range []struct {
		buffer *Buffer
		offset uint64
	}{{vertex, 512}, {instance, 128}} {
		binding := bindings[uint32(slot)]
		if binding == nil || binding.buffer != want.buffer || binding.offset != want.offset || binding.layout != &second.vertexBuffers[slot] {
			t.Fatalf("slot %d kept stale vertex state: %#v", slot, binding)
		}
	}
}
