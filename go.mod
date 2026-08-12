module github.com/gopact-ai/gopact

go 1.27

require github.com/gopact-ai/acp v0.1.0

require github.com/valyala/fastjson v1.6.10 // indirect

retract (
	v0.1.0-rc.5 // Published with ineffective prerelease-only retraction metadata; VCS tag removed.
	[v0.1.0-rc.2, v0.1.0-rc.3] // Pre-rewrite artifacts with removed VCS tags; incompatible with current extensions.
	v0.0.58 // Retraction metadata release; do not use as a library version.
)
