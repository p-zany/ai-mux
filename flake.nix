{
  description = "ai-mux - AI Multiplexer proxy";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      nixosModule =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        let
          cfg = config.services.ai-mux;
          yaml = pkgs.formats.yaml { };
          settingsWithDefaults = lib.recursiveUpdate {
            state_dir = cfg.stateDir;
          } cfg.settings;
          generatedConfig = yaml.generate "ai-mux.yaml" settingsWithDefaults;
          settingsFile = if cfg.settingsFile != null then cfg.settingsFile else generatedConfig;
          args = lib.optionals (settingsFile != null) [
            "--config"
            settingsFile
          ];
          pkgDefault = self.packages.${pkgs.system}.ai-mux;
        in
        {
          options.services.ai-mux = {
            enable = lib.mkEnableOption "ai-mux proxy service";

            package = lib.mkOption {
              type = lib.types.package;
              default = pkgDefault;
              defaultText = "ai-mux from this flake";
              description = "The ai-mux package to use.";
            };

            settingsFile = lib.mkOption {
              type = lib.types.nullOr lib.types.path;
              default = null;
              description = "Path to ai-mux configuration file; if unset, uses settings to generate one or falls back to defaults.";
            };

            settings = lib.mkOption {
              type = lib.types.submodule {
                freeformType = yaml.type;
              };
              default = { };
              description = "ai-mux configuration rendered as YAML when no explicit settingsFile is provided.";
            };

            stateDir = lib.mkOption {
              type = lib.types.str;
              default = "/var/lib/ai-mux";
              description = "Directory for ai-mux state and credentials (default /var/lib/ai-mux).";
            };

            user = lib.mkOption {
              type = lib.types.str;
              default = "ai-mux";
              description = "User to run the ai-mux service.";
            };

            group = lib.mkOption {
              type = lib.types.str;
              default = "ai-mux";
              description = "Group to run the ai-mux service.";
            };
          };

          config = lib.mkIf cfg.enable {
            users.groups = lib.mkIf (cfg.group == "ai-mux") {
              ai-mux = { };
            };
            users.users = lib.mkIf (cfg.user == "ai-mux") {
              ai-mux = {
                inherit (cfg) group;
                description = "ai-mux Service";
                isSystemUser = true;
              };
            };

            systemd.services.ai-mux = {
              description = "ai-mux proxy";
              after = [ "network-online.target" ];
              wants = [ "network-online.target" ];
              wantedBy = [ "multi-user.target" ];

              serviceConfig = {
                User = cfg.user;
                Group = cfg.group;
                WorkingDirectory = cfg.stateDir;
                StateDirectory = lib.mkIf (cfg.stateDir == "/var/lib/ai-mux") "ai-mux";
                Restart = "on-failure";
                RestartSec = "2s";
                ExecStart = lib.concatStringsSep " " (
                  [
                    "${cfg.package}/bin/ai-mux"
                  ]
                  ++ lib.optional (args != [ ]) (lib.escapeShellArgs args)
                );
              };
            };
          };
        };

      overlay = final: _prev: {
        inherit (self.packages.${final.system}) ai-mux;
      };
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        inherit (pkgs) lib;

        pname = "ai-mux";
        version =
          let
            sourceRef = if self ? sourceInfo && self.sourceInfo ? ref then self.sourceInfo.ref else "";
            tagMatch = lib.match "refs/tags/v(.+)" sourceRef;
          in
          if tagMatch != null then
            builtins.elemAt tagMatch 0
          else if self ? shortRev then
            "0.0.0-${self.shortRev}"
          else
            "0.0.0-dev";

        package = pkgs.buildGoModule {
          inherit pname version;
          src = ./.;
          subPackages = [ "cmd/ai-mux" ];
          vendorHash = "sha256-JnikQHCeCIY/1AXObDGxD+28Ff6pct2GGMvv12cQm8M=";
          ldflags = [
            "-s"
            "-w"
          ];
          meta = {
            description = "AI Multiplexer proxy";
            mainProgram = pname;
            platforms = lib.platforms.unix;
          };
        };
      in
      {
        packages = {
          default = package;
          ai-mux = package;
        };

        apps.default = {
          type = "app";
          program = "${package}/bin/ai-mux";
          # App metadata would be nice-to-have, but not required.
        };
      }
    )
    // {
      overlays.default = overlay;
      nixosModules.default = nixosModule;
    };
}
