# Changelog

## [1.1.0](https://github.com/Learistocats/LookupKB/compare/v1.0.1...v1.1.0) (2026-10-24)


### Features

* delete drafts (LOO-58) ([8fac38d](https://github.com/Learistocats/LookupKB/commit/8fac38d4c2538f57356039ebef3dda38c6f0c262))
* delete drafts (LOO-58) ([d05a9f2](https://github.com/Learistocats/LookupKB/commit/d05a9f22dc1fe89ee2fcec7ae10e1944f7bd5eb9))
* MCP server for agents (LOO-59) ([070bc3f](https://github.com/Learistocats/LookupKB/commit/070bc3f3e5a70f66e13b733b2f5048b82cdec4e7))
* MCP server for agents (LOO-59) ([8411432](https://github.com/Learistocats/LookupKB/commit/84114322fb6aad455b3aff1a360c9873ae9b0839))
* personal access tokens (LOO-37, LOO-42) ([e072cb1](https://github.com/Learistocats/LookupKB/commit/e072cb1462fa40cb459d2969024679e24a89eff0))
* personal access tokens (LOO-37, LOO-42) ([12e097f](https://github.com/Learistocats/LookupKB/commit/12e097f64ff67b8053df9ff349f4f0926c1cac3c))
* protected tags access control (LOO-33, LOO-34) ([3623dca](https://github.com/Learistocats/LookupKB/commit/3623dca42f8d9db3757698f77602976c9be40d3f))
* protected tags access control (LOO-33, LOO-34) ([e78897e](https://github.com/Learistocats/LookupKB/commit/e78897e99293853e8335f814152c8b229bfa7fb4))
* public read API (LOO-38) ([d709092](https://github.com/Learistocats/LookupKB/commit/d7090923f403345c2dfde90de307cd7fd3821bed))
* public read API (LOO-38) ([895072d](https://github.com/Learistocats/LookupKB/commit/895072d0b4d8fbfc8cb57d036c703f18656fa63b))
* super-admin role and group management (LOO-56) ([561bcc7](https://github.com/Learistocats/LookupKB/commit/561bcc7285ff5e3f8cca5776c4e6fb013b836574))
* super-admin role and group management (LOO-56) ([a81a295](https://github.com/Learistocats/LookupKB/commit/a81a29590c9fc2372d57fe4bc921e202996f71a5))


### Bug Fixes

* **api:** bound the tag search limit before the int32 conversion ([096e5b5](https://github.com/Learistocats/LookupKB/commit/096e5b5017be229e3cc55bb77dbb1c6ac04b849d))
* **api:** bound the tag search limit before the int32 conversion ([14bb19a](https://github.com/Learistocats/LookupKB/commit/14bb19a1da45740b91ec1b7ce94b1d07a9270191))
* **api:** parse query integers as int32 ([af259da](https://github.com/Learistocats/LookupKB/commit/af259da30e8d79bae76567f3fa281a17884bbb44))
* **api:** parse query integers as int32 ([b1ff109](https://github.com/Learistocats/LookupKB/commit/b1ff10967ec5e7cf70104d170e3dd450aa4e0c61))
* **api:** render returned fiber errors as JSON so helper errors stop the handler ([7ae68ce](https://github.com/Learistocats/LookupKB/commit/7ae68cebbc911878f8c771e0d2cca448e3a597e3))
* **deps:** bump Go toolchain to 1.26.10 (GO-2026-6809) ([f56811b](https://github.com/Learistocats/LookupKB/commit/f56811b0d7c042ad992457d556e41ba350b6784e))
* **deps:** bump Go toolchain to 1.26.10 for GO-2026-6809 ([49343b7](https://github.com/Learistocats/LookupKB/commit/49343b7275449c4dd03d7cde723bf5b73aab270e))
* keep sidebar row actions inside the panel (LOO-58) ([e8920e3](https://github.com/Learistocats/LookupKB/commit/e8920e38eb319c9185358bc074455c0e5fd0ea1f))
* place the sidebar delete button next to the draft chip (LOO-58) ([88b686c](https://github.com/Learistocats/LookupKB/commit/88b686caa1df3febfee052a42d05240f90d89bba))


### Refactoring

* **api:** batch search worker projections, set-based bootstrap, published catalog index ([313e032](https://github.com/Learistocats/LookupKB/commit/313e032ac047e0d33be0a5208d128ad043fbe133))
* **api:** consolidate server HTTP helpers, catalog integrity and MCP bridge ([810e0f3](https://github.com/Learistocats/LookupKB/commit/810e0f37e48b7107cde992080e504d52bb0e9ee3))
* **api:** finish helper dedup ([9460c4a](https://github.com/Learistocats/LookupKB/commit/9460c4a8577e6ff84846fd8bc4ecc4add23e6ec5))
* **api:** one explicit auth store, 1-query PAT path, drop per-request group load ([93ea9ea](https://github.com/Learistocats/LookupKB/commit/93ea9ea1e085d04947e6757020f5d644b26d2dc4))
* **api:** share db helpers, drop dead code, single settings read ([b23f73f](https://github.com/Learistocats/LookupKB/commit/b23f73f940b4dd0d609041053e481ddf4e44f13d))
* **api:** wire markdown cap, drop diagram seam and Kroki/Garage readiness gates ([b68d823](https://github.com/Learistocats/LookupKB/commit/b68d8231ef6c9eec890788e1129f7385f36f4ead))
* code-quality audit cleanup ([19cb782](https://github.com/Learistocats/LookupKB/commit/19cb782672df6877bd5da3420d24514e564bf125))
* **deploy:** dedupe compose config and Makefile recipes ([ec0b6dd](https://github.com/Learistocats/LookupKB/commit/ec0b6ddc1104886f68b886dbce0ea6f277fb9edf))
* **web:** dedupe dialogs, shared UI primitives and theme tokens ([1a13da4](https://github.com/Learistocats/LookupKB/commit/1a13da495b47c9828c12d306ae33fbf4c5ab13a9))
* **web:** fix publish test import ([577f35f](https://github.com/Learistocats/LookupKB/commit/577f35fce24517252a3c211f4d37445a56ac98cc))
* **web:** read the panel cookie through PANEL_COOKIE ([08b7ee5](https://github.com/Learistocats/LookupKB/commit/08b7ee509a26d495e2cf7241cdf5e6ea5f8a42e3))
* **web:** share API error mapping, split /me from entries load, drop dead code ([c5d7790](https://github.com/Learistocats/LookupKB/commit/c5d77906fe0d7983ecbad024a1c3802c436c5ddc))

## [1.0.1](https://github.com/Learistocats/LookupKB/compare/v1.0.0...v1.0.1) (2026-10-20)


### Bug Fixes

* make the release bundle self-contained ([7e0a566](https://github.com/Learistocats/LookupKB/commit/7e0a566eadd96130366c931d07b80c940ff01f0b))
* make the release bundle self-contained ([843d06c](https://github.com/Learistocats/LookupKB/commit/843d06ca8689595c9ae1e4eefae634bd39006349))

## 1.0.0 (2026-10-20)


### Features

* lightweight stack, Meilisearch search and open-source readiness ([6618487](https://github.com/Learistocats/LookupKB/commit/6618487402348d56ca6d00dc39cdb55372dfc795))


### Bug Fixes

* **deps:** patch vulnerable Go and web dependencies ([8779406](https://github.com/Learistocats/LookupKB/commit/8779406bada64f99bd457eb78fc7adbadc8ef019))
