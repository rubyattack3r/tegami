{
  description = "Tegami: encrypted file deliveries that open offline in a browser";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: nixpkgs.legacyPackages.${system};
      version = "0.1.0";
      packagesFor =
        system:
        let
          pkgs = pkgsFor system;
          tegami = pkgs.buildGoModule {
            pname = "tegami";
            inherit version;

            src = pkgs.lib.cleanSource ./.;
            vendorHash = "sha256-RGNjgNF0u7vhH/49gqbDNGKuGvs2dyFS7u2cjM0VSP0=";
            subPackages = [ "cmd/tegami" ];

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];

            # buildGoModule only tests subPackages by default; check everything.
            doCheck = true;
            checkPhase = ''
              runHook preCheck
              go vet ./...
              go test ./...
              runHook postCheck
            '';

            meta = {
              description = "Encrypted file deliveries that open offline in a browser";
              homepage = "https://github.com/rubyattack3r/tegami";
              license = pkgs.lib.licenses.mit;
              mainProgram = "tegami";
              platforms = pkgs.lib.platforms.unix;
            };
          };
        in
        {
          default = tegami;
          inherit tegami;
        };
    in
    {
      packages = forAllSystems packagesFor;

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/tegami";
          meta.description = "Create encrypted deliveries or the offline Studio";
        };
      });

      checks = forAllSystems (system: {
        inherit (self.packages.${system}) tegami;
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
          default = pkgs.mkShell {
            packages = [
              pkgs.actionlint
              pkgs.go
              pkgs.golangci-lint
              pkgs.goreleaser
              pkgs.gopls
              pkgs.gotools
              pkgs.govulncheck
              pkgs.jq
              # Builds the Argon2id WebAssembly module; see wasm/.
              pkgs.llvmPackages.clang-unwrapped
              pkgs.llvmPackages.lld
              pkgs.vhs
              pkgs.nixfmt
            ];
          };
        in
        {
          inherit default;
        }
        # `make browser-test` with a pinned Chromium. Kept out of the default
        # shell, which every CI job loads; nixpkgs has no macOS Chromium.
        // nixpkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          browser = pkgs.mkShell {
            inputsFrom = [ default ];
            packages = [ pkgs.chromium ];
          };
        }
      );

      formatter = forAllSystems (system: (pkgsFor system).nixfmt);
    };
}
