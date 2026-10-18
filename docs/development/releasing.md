# Releasing

Pushing a `v*` tag runs `.github/workflows/release.yml`, which publishes a
GitHub release with [GoReleaser](https://goreleaser.com/). Versions follow
[semantic versioning](https://semver.org/).

## Cutting a release

1. In `CHANGELOG.md`, rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`
   and add a fresh, empty `## [Unreleased]` above it. The release fails if the
   changelog has no section for the tag.
2. Set `version = "X.Y.Z"` in `flake.nix`.
3. Run the local gate, then commit and merge to `main`:

   ```sh
   nix develop --command make check
   nix develop --command make browser-test
   nix flake check
   ```

4. Tag the merged commit and push the tag:

   ```sh
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

The workflow then:

- writes the Studio to `build/tegami.html` (`make studio`);
- cross-compiles `tegami` for Linux, macOS, and Windows on `amd64` and
  `arm64` with the `toolchain` version from `go.mod`;
- publishes archives, `tegami.html`, and `checksums.txt`, with the
  changelog section as release notes;
- attests build provenance for every asset, verifiable with
  `gh attestation verify`.

## Testing locally

```sh
nix develop --command goreleaser check
nix develop --command goreleaser release --snapshot --clean
```

Artifacts land in `dist/` and the Studio in `build/tegami.html`. Both are
git-ignored.

## Toolchain updates

`make vulncheck` runs in CI weekly and fails when the Go standard library has
a reachable vulnerability. When it does, raise the `toolchain` line in
`go.mod` to the fixed release so published binaries pick it up, and run
`nix flake update` once nixpkgs ships the same version.
