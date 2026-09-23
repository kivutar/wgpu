# Goro OpenGL fixes

This branch is based on upstream `github.com/gogpu/wgpu` v0.34.3 and contains
OpenGL backend fixes tested with Goro. The module path remains unchanged so
consumers can use a versioned Go module replacement to pin this fork.

In `hal/gles/gl/context_linux.go`, `glDeleteSync`
uses a `void(void*)` foreign-function signature instead of `void(uint32)`.
`GLsync` is an opaque pointer. The old signature truncates it on 64-bit Linux,
causing `GL_INVALID_VALUE` during fence deletion. The next texture allocation
then reports that stale error as a `TexImage2D` failure.

In `hal/gles/command.go`, pipeline changes restore previously bound vertex
buffers using the new pipeline's attribute layouts and the saved buffer offsets.
This supports binding buffers before selecting a pipeline and switching between
world and screen layouts without rebinding buffers, as allowed by WebGPU. Without
this, Goro's screen draws can have missing or incorrect attributes and render black.
Regression tests cover binding order and pipeline changes with multiple buffers.

Indexed draws now honor `BaseVertex` and the byte offset passed to
`SetIndexBuffer`. Goro packs map meshes into shared buffer pages; ignoring the
base vertex makes later meshes read unrelated vertices and renders stretched,
scrambled triangles. The Linux and Windows GL bindings now expose the base-vertex
draw functions (core desktop OpenGL 3.2+), including instanced draws. A Linux GPU
integration test checks rendered pixels with positive and negative base vertices,
both index formats, nonzero index offsets, and instancing.
Linux selects the core, OES, or EXT entry points using the current context's
version and advertised extensions, since a non-null EGL dispatch stub does not
prove support. Missing required functions reject initialization with an error.
The pixel tests also exercise OES-only and EXT-only entry points on GLES; they
can be run with `MESA_GLES_VERSION_OVERRIDE=3.0` to test the older ES version.

The Linux bindings for `glClearDepth` and `glDepthRange` now use double-precision
arguments on desktop OpenGL, preserving the float signatures of the GLES `f`
variants. Passing float bit patterns as doubles collapsed Goro's depth range to
zero, so later surfaces overwrote earlier surfaces regardless of distance.

Bind groups now apply a shared sampler to every texture unit that uses it, rather
than stopping at the first texture. This restores linear filtering for Goro's
lightmaps, which share a sampler with the base texture. GPU integration tests
check depth occlusion on desktop GL and GLES, desktop depth values, and the actual
sampler bindings for multiple textures. The old sampler lookup test duplicated
the incorrect implementation and is replaced by the GPU binding test.
Sampler mappings include both vertex and fragment shader reflection on Linux
and Windows. Tests compile real pipelines with a vertex-only sampler and with
a sampler shared by textures in both stages, then verify their GL bindings.

Run the Linux GPU regressions (requires EGL/OpenGL) from this repository with:

```sh
CGO_ENABLED=1 go test -tags integration ./hal/gles \
  -run '^(TestIndexedDrawOffsets|TestBaseVertexFunctionsRequired|TestVertexStageSamplerBinding|TestDepthOcclusion|TestSharedSamplerBinding)$'
```

Consumers can remove their module replacement when upgrading to an upstream
release containing these fixes.
