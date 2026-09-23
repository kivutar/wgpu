// Copyright 2026 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build integration && linux && !(js && wasm)

package gles

import (
	"strings"
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/naga/glsl"
	"github.com/gogpu/wgpu/hal"
	"github.com/gogpu/wgpu/hal/gles/gl"
)

func TestVertexStageSamplerBinding(t *testing.T) {
	for _, mode := range []string{"vertex-only", "shared-with-fragment"} {
		t.Run(mode, func(t *testing.T) { testVertexStageSamplerBinding(t, mode == "shared-with-fragment") })
	}
}

func testVertexStageSamplerBinding(t *testing.T, shared bool) {
	g := depthTestContext(t, false)
	d := &Device{glCtx: g, glslVersion: glsl.Version330}
	bgl := &BindGroupLayout{entries: []gputypes.BindGroupLayoutEntry{
		{Binding: 0, Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
		{Binding: 1, Visibility: gputypes.ShaderStageVertex, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
		{Binding: 2, Visibility: gputypes.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
	}}
	layout, err := d.CreatePipelineLayout(&PipelineLayoutDescriptor{BindGroupLayouts: []hal.BindGroupLayout{bgl}})
	if err != nil {
		t.Fatal(err)
	}
	module := &ShaderModule{source: hal.ShaderSource{WGSL: `
@group(0) @binding(0) var s: sampler;
@group(0) @binding(1) var tex: texture_2d<f32>;
@group(0) @binding(2) var fragment_tex: texture_2d<f32>;
struct Output { @builtin(position) pos: vec4<f32>, @location(0) color: vec4<f32> }
@vertex fn vs_main() -> Output {
 var out: Output;
 out.pos=vec4<f32>(0.0,0.0,0.0,1.0);
 out.color=textureSampleLevel(tex,s,vec2<f32>(0.5,0.5),0.0);
 return out;
}
@fragment fn fs_main(input: Output) -> @location(0) vec4<f32> { return input.color; }
`}}
	if shared {
		module.source.WGSL = strings.Replace(module.source.WGSL, "return input.color;", "return input.color * textureSample(fragment_tex,s,vec2<f32>(0.5,0.5));", 1)
	}
	pipeline, err := d.CreateRenderPipeline(&RenderPipelineDescriptor{
		Layout: layout, Vertex: hal.VertexState{Module: module, EntryPoint: "vs_main"},
		Fragment:  &hal.FragmentState{Module: module, EntryPoint: "fs_main", Targets: []gputypes.ColorTargetState{{Format: gputypes.TextureFormatRGBA8Unorm, WriteMask: gputypes.ColorWriteMaskAll}}},
		Primitive: gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Destroy()
	sampler := g.GenSamplers(1)
	defer g.DeleteSamplers(sampler)
	encoder := &CommandEncoder{}
	pass := &RenderPassEncoder{encoder: encoder}
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, &BindGroup{layout: bgl, entries: []gputypes.BindGroupEntry{{Binding: 0, Resource: gputypes.SamplerBinding{Sampler: uintptr(sampler)}}}}, nil)
	for _, cmd := range encoder.commands {
		cmd.Execute(g)
	}
	for unit := uint32(0); unit < 2; unit++ {
		g.ActiveTexture(gl.TEXTURE0 + unit)
		var bound int32
		g.GetIntegerv(0x8919, &bound)
		want := int32(sampler)
		if unit == 1 && !shared {
			want = 0
		}
		if bound != want {
			t.Errorf("texture unit %d has sampler %d, want %d", unit, bound, want)
		}
	}
	if err := g.GetError(); err != gl.NO_ERROR {
		t.Fatalf("OpenGL error: %#x", err)
	}
}
