//go:build !(js && wasm)

package vulkan

import (
	"testing"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu/hal/vulkan/vk"
)

func TestRenderTextureTransitionSourceMatchesRenderPass(t *testing.T) {
	for _, tc := range []struct {
		name    string
		usage   gputypes.TextureUsage
		samples uint32
		want    vk.ImageLayout
	}{
		{"sampled scene", gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding, 1, vk.ImageLayoutGeneral},
		{"plain attachment", gputypes.TextureUsageRenderAttachment, 1, vk.ImageLayoutColorAttachmentOptimal},
		{"MSAA source", gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding, 4, vk.ImageLayoutColorAttachmentOptimal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tex := &Texture{usage: tc.usage, samples: tc.samples, format: gputypes.TextureFormatRGBA8Unorm}
			access, _, layout := textureUsageToAccessStageLayout(gputypes.TextureUsageRenderAttachment)
			if access&vk.AccessFlags(vk.AccessColorAttachmentWriteBit) == 0 {
				t.Fatal("source must synchronize color writes")
			}
			if got := textureTransitionSourceLayout(tex, gputypes.TextureUsageRenderAttachment, layout); got != tc.want {
				t.Fatalf("layout=%v, want %v", got, tc.want)
			}
			// Upload/copy transitions on the same image must keep their own layout.
			if got := textureTransitionSourceLayout(tex, gputypes.TextureUsageCopyDst, vk.ImageLayoutTransferDstOptimal); got != vk.ImageLayoutTransferDstOptimal {
				t.Fatal("copy layout changed")
			}
		})
	}
	tex := &Texture{usage: gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding, samples: 1, format: gputypes.TextureFormatDepth32Float}
	if got := textureTransitionSourceLayout(tex, gputypes.TextureUsageRenderAttachment, vk.ImageLayoutDepthStencilAttachmentOptimal); got != vk.ImageLayoutDepthStencilAttachmentOptimal {
		t.Fatal("depth attachment treated as color")
	}
}
