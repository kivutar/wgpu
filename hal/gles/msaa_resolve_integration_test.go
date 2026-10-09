// Copyright 2026 The GoGPU Authors
// SPDX-License-Identifier: MIT

//go:build integration && linux && !(js && wasm)

package gles

import (
	"testing"
	"unsafe"

	"github.com/gogpu/wgpu/hal"
	"github.com/gogpu/wgpu/hal/gles/gl"
)

func TestMSAAResolveToNewTexture(t *testing.T) {
	for _, api := range []struct {
		name string
		es   bool
	}{{"desktop", false}, {"ES", true}} {
		t.Run(api.name, func(t *testing.T) {
			g := depthTestContext(t, api.es)
			var maxSamples int32
			g.GetIntegerv(gl.MAX_SAMPLES, &maxSamples)
			if maxSamples < 4 {
				t.Skip("4x MSAA unavailable")
			}
			makeTexture := func(target uint32) *Texture {
				tex := &Texture{id: g.GenTextures(1), target: target, size: hal.Extent3D{Width: 16, Height: 16, DepthOrArrayLayers: 1}}
				t.Cleanup(func() {
					if tex.fbo != 0 {
						g.DeleteFramebuffers(tex.fbo)
					}
					g.DeleteTextures(tex.id)
				})
				g.BindTexture(target, tex.id)
				if target == gl.TEXTURE_2D_MULTISAMPLE {
					g.TexImage2DMultisample(target, 4, gl.RGBA8, 16, 16, true)
				} else {
					g.TexImage2D(target, 0, gl.RGBA8, 16, 16, 0, gl.RGBA, gl.UNSIGNED_BYTE, 0)
				}
				return tex
			}
			source := makeTexture(gl.TEXTURE_2D_MULTISAMPLE)
			target := makeTexture(gl.TEXTURE_2D)
			// The first resolve creates the destination FBO; the second reuses it.
			for _, want := range [][4]byte{{255, 0, 0, 255}, {0, 255, 0, 255}} {
				(&EnsureOffscreenFBOCommand{texture: source}).Execute(g)
				g.ClearColor(float32(want[0])/255, float32(want[1])/255, 0, 1)
				g.Clear(gl.COLOR_BUFFER_BIT)
				(&MSAAResolveCommand{msaaTexture: source, resolveTexture: target, width: 16, height: 16}).Execute(g)
				g.BindFramebuffer(gl.FRAMEBUFFER, target.fbo)
				var got [4]byte
				g.ReadPixels(8, 8, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&got[0]))
				if err := g.GetError(); err != gl.NO_ERROR {
					t.Fatalf("GL error %#x", err)
				}
				if got != want {
					t.Fatalf("resolved pixel=%v, want %v", got, want)
				}
			}
		})
	}
}
