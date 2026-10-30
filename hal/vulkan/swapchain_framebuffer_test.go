//go:build !(js && wasm)

package vulkan

import (
	"testing"

	"github.com/gogpu/wgpu/hal/vulkan/vk"
)

func TestSwapchainRetirementInvalidatesFramebuffers(t *testing.T) {
	// Unloaded commands make Vulkan destruction a no-op, so this exercises
	// the real retirement/cache path without needing a GPU or native window.
	cmds := vk.NewCommands()
	cache := NewRenderPassCache(0, cmds)
	color := FramebufferKey{RenderPass: 1, Width: 800, Height: 600, ViewCount: 1}
	color.Views[0] = 10
	resolve := FramebufferKey{RenderPass: 2, Width: 800, Height: 600, ViewCount: 2}
	resolve.Views[0], resolve.Views[1] = 20, 11
	offscreen := FramebufferKey{RenderPass: 3, Width: 800, Height: 600, ViewCount: 1}
	offscreen.Views[0] = 20
	cache.framebuffers[color] = 100
	cache.framebuffers[resolve] = 101
	cache.framebuffers[offscreen] = 102
	device := &Device{cmds: cmds, renderPassCache: cache}
	swapchain := &Swapchain{device: device, imageViews: []vk.ImageView{10, 11}}
	device.queue = &Queue{device: device, activeSwapchain: swapchain}

	if err := retireOldSwapchain(device, swapchain, 0); err != nil {
		t.Fatal(err)
	}
	// A replacement swapchain can reuse the same view handles and dimensions.
	// Neither of its keys may still hit a framebuffer from the old swapchain.
	for _, key := range []FramebufferKey{color, resolve} {
		if stale, ok := cache.framebuffers[key]; ok {
			t.Errorf("retired swapchain framebuffer %d remains cached", stale)
		}
	}
	if cache.framebuffers[offscreen] != 102 || len(cache.framebuffers) != 1 {
		t.Errorf("remaining framebuffers = %v, want only the offscreen framebuffer", cache.framebuffers)
	}
}
