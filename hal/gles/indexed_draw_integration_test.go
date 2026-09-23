// Copyright 2026 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build integration && linux && !(js && wasm)

package gles

import (
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu/hal/gles/egl"
	"github.com/gogpu/wgpu/hal/gles/gl"
)

// TestIndexedDrawOffsets checks actual pixels: recording the right draw arguments
// alone does not catch a backend that ignores them at execution time.
func TestIndexedDrawOffsets(t *testing.T) {
	for _, variant := range []struct {
		name   string
		es     bool
		suffix string
	}{
		{"desktop", false, ""}, {"ES", true, ""}, {"OES", true, "OES"}, {"EXT", true, "EXT"},
	} {
		t.Run(variant.name, func(t *testing.T) { testIndexedDrawOffsets(t, variant.es, variant.suffix) })
	}
}

func testIndexedDrawOffsets(t *testing.T, es bool, suffix string) {
	g := depthTestContext(t, es)
	if suffix != "" {
		var count int32
		g.GetIntegerv(gl.NUM_EXTENSIONS, &count)
		supported := false
		for i := int32(0); i < count; i++ {
			if g.GetStringi(gl.EXTENSIONS, uint32(i)) == "GL_"+suffix+"_draw_elements_base_vertex" {
				supported = true
			}
		}
		if !supported {
			t.Skipf("%s base-vertex extension is unavailable", suffix)
		}
		selected := 0
		// Real extension functions remain available; hide the core and other suffix.
		err := g.Load(func(name string) unsafe.Pointer {
			if strings.HasPrefix(name, "glDrawElementsBaseVertex") || strings.HasPrefix(name, "glDrawElementsInstancedBaseVertex") {
				if !strings.HasSuffix(name, suffix) {
					return nil
				}
				selected++
			}
			return egl.GetGLProcAddress(name)
		}, es)
		if err != nil {
			t.Fatal(err)
		}
		if selected != 2 {
			t.Fatalf("selected %d extension functions, want 2", selected)
		}
	}
	version := "#version 330 core\n"
	if es {
		version = "#version 300 es\nprecision highp float;\n"
	}

	program := g.CreateProgram()
	defer g.DeleteProgram(program)
	for stage, source := range map[uint32]string{
		gl.VERTEX_SHADER: version + `
layout(location=0) in vec2 position;
void main() {
    gl_Position = vec4(position + vec2(-0.4 + 0.8 * float(gl_InstanceID), 0.0), 0.0, 1.0);
}`,
		gl.FRAGMENT_SHADER: version + `
out vec4 color;
void main() { color = vec4(0.0, 1.0, 0.0, 1.0); }`,
	} {
		shader := g.CreateShader(stage)
		defer g.DeleteShader(shader)
		g.ShaderSource(shader, source)
		g.CompileShader(shader)
		var compiled int32
		g.GetShaderiv(shader, gl.COMPILE_STATUS, &compiled)
		if compiled == 0 {
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

	texture := g.GenTextures(1)
	defer g.DeleteTextures(texture)
	g.BindTexture(gl.TEXTURE_2D, texture)
	g.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 16, 16, 0, gl.RGBA, gl.UNSIGNED_BYTE, 0)
	fbo := g.GenFramebuffers(1)
	defer g.DeleteFramebuffers(fbo)
	g.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	g.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0)
	if status := g.CheckFramebufferStatus(gl.FRAMEBUFFER); status != gl.FRAMEBUFFER_COMPLETE {
		t.Fatalf("framebuffer status: %#x", status)
	}
	g.Viewport(0, 0, 16, 16)
	g.ClearColor(0, 0, 0, 1)

	vao := g.GenVertexArrays(1)
	defer g.DeleteVertexArrays(vao)
	g.BindVertexArray(vao)
	vbo := g.GenBuffers(1)
	defer g.DeleteBuffers(vbo)
	g.BindBuffer(gl.ARRAY_BUFFER, vbo)
	// A degenerate offscreen triangle precedes the visible mesh in the same buffer.
	vertices := []float32{-2, -2, -2, -2, -2, -2, -.35, -.6, .35, -.6, 0, .6}
	g.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, uintptr(unsafe.Pointer(&vertices[0])), gl.STATIC_DRAW)
	runtime.KeepAlive(vertices)
	g.EnableVertexAttribArray(0)
	ebo := g.GenBuffers(1)
	defer g.DeleteBuffers(ebo)
	g.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, ebo)

	for _, format := range []gputypes.IndexFormat{gputypes.IndexFormatUint16, gputypes.IndexFormatUint32} {
		for _, base := range []int32{0, 3, -3} {
			for _, instances := range []uint32{1, 2} {
				t.Logf("format=%d base=%d instances=%d", format, base, instances)
				vertexOffset := uintptr(0)
				if base <= 0 {
					vertexOffset = 3 * 2 * 4
				}
				g.VertexAttribPointer(0, 2, gl.FLOAT, false, 8, vertexOffset)
				indexSize := 2
				if format == gputypes.IndexFormatUint32 {
					indexSize = 4
				}
				// Skip eight bound bytes plus one index. Missing either offset reads
				// the zero padding, yielding a degenerate triangle.
				indices := make([]byte, 8+4*indexSize)
				for i := 0; i < 3; i++ {
					value := uint32(i)
					if base < 0 {
						value += 3
					}
					dst := indices[8+(i+1)*indexSize:]
					if indexSize == 2 {
						binary.LittleEndian.PutUint16(dst, uint16(value))
					} else {
						binary.LittleEndian.PutUint32(dst, value)
					}
				}
				g.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(indices), uintptr(unsafe.Pointer(&indices[0])), gl.STATIC_DRAW)
				runtime.KeepAlive(indices)
				g.Clear(gl.COLOR_BUFFER_BIT)
				encoder := &CommandEncoder{}
				pass := &RenderPassEncoder{encoder: encoder}
				pass.SetIndexBuffer(&Buffer{id: ebo}, format, 8)
				pass.DrawIndexed(gputypes.DrawIndexedArgs{
					IndexCount: 3, InstanceCount: instances, FirstIndex: 1, BaseVertex: base,
				})
				for _, command := range encoder.commands {
					command.Execute(g)
				}
				for _, x := range []int32{5, 11} {
					var pixel [4]byte
					g.ReadPixels(x, 7, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&pixel[0]))
					want := byte(255)
					if x == 11 && instances == 1 {
						want = 0
					}
					if pixel[0] != 0 || pixel[1] != want || pixel[2] != 0 {
						t.Errorf("pixel x=%d: %v, want green=%d", x, pixel, want)
					}
				}
				if err := g.GetError(); err != gl.NO_ERROR {
					t.Fatalf("OpenGL error: %#x", err)
				}
			}
		}
	}
}

// Missing either entry point must reject initialization rather than silently
// skipping nonzero-base draws. Test both missing functions independently.
func TestBaseVertexFunctionsRequired(t *testing.T) {
	g := depthTestContext(t, true)
	for _, missing := range []string{"glDrawElementsBaseVertex", "glDrawElementsInstancedBaseVertex"} {
		err := g.Load(func(name string) unsafe.Pointer {
			if strings.HasPrefix(name, missing) {
				return nil
			}
			return egl.GetGLProcAddress(name)
		}, true)
		if err == nil || !strings.Contains(err.Error(), "base-vertex") {
			t.Fatalf("missing %s: error=%v, want unsupported base-vertex drawing", missing, err)
		}
	}
}
