// Copyright 2026 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build integration && linux && !(js && wasm)

package gles

import (
	"math"
	"runtime"
	"testing"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu/hal/gles/egl"
	"github.com/gogpu/wgpu/hal/gles/gl"
)

func depthTestContext(t *testing.T, es bool) *gl.Context {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	if err := egl.Init(); err != nil {
		t.Fatal(err)
	}
	config := egl.DefaultContextConfig()
	kind := egl.WindowKindSurfaceless
	config.WindowKind = &kind
	config.GLES = es
	if es {
		config.GLVersionMinor = 0
	}
	ctx, err := egl.NewContext(config)
	if err != nil {
		t.Skipf("OpenGL context unavailable: %v", err)
	}
	t.Cleanup(ctx.Destroy)
	if err := ctx.MakeCurrent(); err != nil {
		t.Fatal(err)
	}
	g := &gl.Context{}
	if err := g.Load(egl.GetGLProcAddress, es); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestDepthOcclusion(t *testing.T) {
	for _, api := range []struct {
		name string
		es   bool
	}{{"desktop", false}, {"ES", true}} {
		t.Run(api.name, func(t *testing.T) {
			g := depthTestContext(t, api.es)
			version := "#version 330 core\n"
			if api.es {
				version = "#version 300 es\nprecision highp float;\n"
			}
			program := g.CreateProgram()
			t.Cleanup(func() { g.DeleteProgram(program) })
			for stage, source := range map[uint32]string{
				gl.VERTEX_SHADER: `
flat out vec4 color;
void main() {
    vec2 positions[3] = vec2[3](vec2(-1.0, -1.0), vec2(3.0, -1.0), vec2(-1.0, 3.0));
    bool front = gl_VertexID < 3;
    gl_Position = vec4(positions[gl_VertexID % 3], front ? -0.5 : 0.5, 1.0);
    color = front ? vec4(1.0, 0.0, 0.0, 1.0) : vec4(0.0, 1.0, 0.0, 1.0);
}`,
				gl.FRAGMENT_SHADER: `
flat in vec4 color;
out vec4 result;
void main() { result = color; }`,
			} {
				shader := g.CreateShader(stage)
				t.Cleanup(func() { g.DeleteShader(shader) })
				g.ShaderSource(shader, version+source)
				g.CompileShader(shader)
				var ok int32
				g.GetShaderiv(shader, gl.COMPILE_STATUS, &ok)
				if ok == 0 {
					t.Fatal(g.GetShaderInfoLog(shader))
				}
				g.AttachShader(program, shader)
			}
			g.LinkProgram(program)
			var linked int32
			g.GetProgramiv(program, gl.LINK_STATUS, &linked)
			if linked == 0 {
				t.Fatal(g.GetProgramInfoLog(program))
			}
			g.UseProgram(program)
			vao := g.GenVertexArrays(1)
			t.Cleanup(func() { g.DeleteVertexArrays(vao) })
			g.BindVertexArray(vao)
			fbo := g.GenFramebuffers(1)
			t.Cleanup(func() { g.DeleteFramebuffers(fbo) })
			g.BindFramebuffer(gl.FRAMEBUFFER, fbo)
			for _, target := range []struct {
				format, attachment uint32
			}{{gl.RGBA8, gl.COLOR_ATTACHMENT0}, {gl.DEPTH_COMPONENT24, gl.DEPTH_ATTACHMENT}} {
				rb := g.GenRenderbuffers(1)
				t.Cleanup(func() { g.DeleteRenderbuffers(rb) })
				g.BindRenderbuffer(gl.RENDERBUFFER, rb)
				g.RenderbufferStorage(gl.RENDERBUFFER, target.format, 8, 8)
				g.FramebufferRenderbuffer(gl.FRAMEBUFFER, target.attachment, gl.RENDERBUFFER, rb)
			}
			if status := g.CheckFramebufferStatus(gl.FRAMEBUFFER); status != gl.FRAMEBUFFER_COMPLETE {
				t.Fatalf("framebuffer status: %#x", status)
			}
			g.Viewport(0, 0, 8, 8)
			g.Enable(gl.DEPTH_TEST)
			g.DepthMask(true)
			g.DepthFunc(gl.LEQUAL)
			g.DepthRange(0.2, 0.8)
			g.ClearColor(0, 0, 0, 1)
			checkDepth := func(want float32) {
				// GLES does not require depth readback; its color assertions still
				// verify both depth clearing and ordering.
				if api.es {
					return
				}
				var depth float32
				g.ReadPixels(4, 4, 1, 1, gl.DEPTH_COMPONENT, gl.FLOAT, unsafe.Pointer(&depth))
				if math.Abs(float64(depth-want)) > 0.00001 {
					t.Errorf("depth = %f, want %f", depth, want)
				}
			}
			for _, clear := range []float64{0.9, 0.25} {
				g.ClearDepth(clear)
				g.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
				checkDepth(float32(clear))
				// Front red, then rear green: green must never overwrite red.
				g.DrawArrays(gl.TRIANGLES, 0, 3)
				g.DrawArrays(gl.TRIANGLES, 3, 3)
				var pixel [4]byte
				g.ReadPixels(4, 4, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&pixel[0]))
				want := [4]byte{255, 0, 0, 255}
				if clear < 0.35 {
					want = [4]byte{0, 0, 0, 255}
					checkDepth(float32(clear))
				} else {
					checkDepth(0.35)
				}
				if pixel != want {
					t.Errorf("clear=%f: pixel=%v, want %v", clear, pixel, want)
				}
			}
			if err := g.GetError(); err != gl.NO_ERROR {
				t.Fatalf("OpenGL error: %#x", err)
			}
		})
	}
}

func TestSharedSamplerBinding(t *testing.T) {
	g := depthTestContext(t, false)
	sampler := g.GenSamplers(1)
	t.Cleanup(func() { g.DeleteSamplers(sampler) })
	var mapping [maxTextureSlots]int8
	for i := range mapping {
		mapping[i] = -1
	}
	mapping[0], mapping[1] = 0, 0 // Base texture and lightmap share a sampler.
	cmd := SetBindGroupCommand{
		group: &BindGroup{entries: []gputypes.BindGroupEntry{{
			Binding: 1, Resource: gputypes.SamplerBinding{Sampler: uintptr(sampler)},
		}}},
		groupInfos:     []BindGroupLayoutInfo{{BindingToSlot: []uint8{0xff, 0}}},
		samplerBindMap: &mapping,
	}
	cmd.Execute(g)
	for unit := uint32(0); unit < 3; unit++ {
		g.ActiveTexture(gl.TEXTURE0 + unit)
		var bound int32
		const samplerBinding = 0x8919 // GL_SAMPLER_BINDING
		g.GetIntegerv(samplerBinding, &bound)
		want := int32(sampler)
		if unit == 2 {
			want = 0 // An unused texture unit must remain untouched.
		}
		if bound != want {
			t.Errorf("unit %d: sampler=%d, want %d", unit, bound, want)
		}
	}
	if err := g.GetError(); err != gl.NO_ERROR {
		t.Fatalf("OpenGL error: %#x", err)
	}
}
